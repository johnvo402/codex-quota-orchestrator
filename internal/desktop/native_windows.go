//go:build windows

package desktop

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	nativeFrameMaxSize = 128 * 1024
	nativeTimeout      = 20 * time.Second
)

type windowsNativeSender struct {
	pipe       string
	executor   string
	source     string
	envHasPipe bool
	parentPID  int
}

type parentProcess struct {
	ProcessID       int    `json:"ProcessId"`
	ParentProcessID int    `json:"ParentProcessId"`
	Name            string `json:"Name"`
	CommandLine     string `json:"CommandLine"`
}

var (
	shell32 = syscall.NewLazyDLL(
		"shell32.dll",
	)

	commandLineToArgvW = shell32.NewProc(
		"CommandLineToArgvW",
	)

	kernel32 = syscall.NewLazyDLL(
		"kernel32.dll",
	)

	localFree = kernel32.NewProc(
		"LocalFree",
	)
)

var codexAppOverrideRE = regexp.MustCompile(
	`(?is)^\s*mcp_servers\.codex_app\s*=\s*(.+)\s*$`,
)

var codexAppPipeRE = regexp.MustCompile(
	`(?is)(?:"CODEX_APP_TOOLS_PIPE_PATH"|CODEX_APP_TOOLS_PIPE_PATH)\s*=\s*("(?:\\.|[^"])*"|'[^']*')`,
)

func newPlatformNativeSender(
	executor string,
) NativeSender {
	pipe, source, envHasPipe := resolveNativePipe()

	return &windowsNativeSender{
		pipe:       pipe,
		executor:   strings.TrimSpace(executor),
		source:     source,
		envHasPipe: envHasPipe,
		parentPID:  os.Getppid(),
	}
}

func (s *windowsNativeSender) Available() bool {
	return s.pipe != "" &&
		s.executor != ""
}

func (s *windowsNativeSender) Description() string {
	if s.pipe == "" {
		return "native pipe unavailable"
	}

	if s.executor == "" {
		return "native pipe found but relay executor unavailable"
	}

	return fmt.Sprintf(
		"%s via %s",
		s.pipe,
		s.source,
	)
}

func (s *windowsNativeSender) Diagnostics() NativeDiagnostics {
	return NativeDiagnostics{
		Available:          s.Available(),
		Source:             s.source,
		Pipe:               s.pipe,
		ExecutorConfigured: s.executor != "",
		EnvironmentHasPipe: s.envHasPipe,
		ParentPID:          s.parentPID,
	}
}

func (s *windowsNativeSender) Probe(
	ctx context.Context,
) error {
	if !s.Available() {
		return fmt.Errorf(
			"Desktop native delivery unavailable: %s",
			s.Description(),
		)
	}

	// list_threads là read-only.
	// Dùng để xác nhận chúng ta thực sự kết nối được
	// Codex Desktop native tools pipe.
	return s.callTool(
		ctx,
		"list_threads",
		map[string]any{
			"limit": 1,
		},
	)
}

func (s *windowsNativeSender) SendMessage(
	ctx context.Context,
	targetThreadID string,
	message string,
) error {
	targetThreadID = strings.TrimSpace(
		targetThreadID,
	)

	message = strings.TrimSpace(
		message,
	)

	if targetThreadID == "" {
		return fmt.Errorf(
			"target thread ID is empty",
		)
	}

	if message == "" {
		return fmt.Errorf(
			"Desktop message is empty",
		)
	}

	return s.callTool(
		ctx,
		"send_message_to_thread",
		map[string]any{
			"threadId": targetThreadID,
			"prompt":   message,
		},
	)
}

func (s *windowsNativeSender) callTool(
	ctx context.Context,
	tool string,
	arguments map[string]any,
) error {
	if !s.Available() {
		return fmt.Errorf(
			"Desktop native delivery unavailable: %s",
			s.Description(),
		)
	}

	if strings.TrimSpace(tool) == "" {
		return fmt.Errorf(
			"native Desktop tool is empty",
		)
	}

	// Windows named pipe có thể mở như file.
	f, err := os.OpenFile(
		s.pipe,
		os.O_RDWR,
		0,
	)

	if err != nil {
		return fmt.Errorf(
			"open Desktop native pipe %q: %w",
			s.pipe,
			err,
		)
	}

	defer f.Close()

	_ = f.SetDeadline(
		time.Now().Add(
			nativeTimeout,
		),
	)

	callID := uuidLike()

	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",

		"params": map[string]any{
			"arguments": arguments,

			"callId": fmt.Sprintf(
				"cdqg-%s",
				callID,
			),

			"namespace": "codex_app",

			// Đây là executor thread,
			// không phải target thread.
			"threadId": s.executor,

			"tool": tool,

			"turnId": fmt.Sprintf(
				"cdqg-turn-%s",
				callID,
			),
		},
	}

	payload, err := json.Marshal(
		request,
	)

	if err != nil {
		return fmt.Errorf(
			"encode native request: %w",
			err,
		)
	}

	if len(payload) == 0 {
		return fmt.Errorf(
			"native request is empty",
		)
	}

	if len(payload) > nativeFrameMaxSize {
		return fmt.Errorf(
			"native request exceeds frame limit: %d bytes",
			len(payload),
		)
	}

	// Native tools pipe:
	//
	// [4 byte UInt32LE length]
	// [UTF-8 JSON]
	var header [4]byte

	binary.LittleEndian.PutUint32(
		header[:],
		uint32(len(payload)),
	)

	if _, err := f.Write(
		header[:],
	); err != nil {
		return fmt.Errorf(
			"write native request header: %w",
			err,
		)
	}

	if _, err := f.Write(
		payload,
	); err != nil {
		return fmt.Errorf(
			"write native request body: %w",
			err,
		)
	}

	if _, err := io.ReadFull(
		f,
		header[:],
	); err != nil {
		return fmt.Errorf(
			"read native response header: %w",
			err,
		)
	}

	responseLength :=
		binary.LittleEndian.Uint32(
			header[:],
		)

	if responseLength == 0 {
		return fmt.Errorf(
			"native response has zero length",
		)
	}

	if responseLength > 1024*1024 {
		return fmt.Errorf(
			"native response too large: %d bytes",
			responseLength,
		)
	}

	response := make(
		[]byte,
		responseLength,
	)

	if _, err := io.ReadFull(
		f,
		response,
	); err != nil {
		return fmt.Errorf(
			"read native response body: %w",
			err,
		)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()

	default:
	}

	if err := validateNativeResponse(
		response,
	); err != nil {
		return fmt.Errorf(
			"native Desktop tool %s failed: %w",
			tool,
			err,
		)
	}

	return nil
}

func validateNativeResponse(
	raw []byte,
) error {
	var response struct {
		JSONRPC string `json:"jsonrpc"`

		ID any `json:"id"`

		Result json.RawMessage `json:"result"`

		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(
		raw,
		&response,
	); err != nil {
		return fmt.Errorf(
			"decode native JSON-RPC response: %w; raw=%s",
			err,
			truncateString(
				string(raw),
				2000,
			),
		)
	}

	if response.JSONRPC != "2.0" {
		return fmt.Errorf(
			"unexpected JSON-RPC version %q",
			response.JSONRPC,
		)
	}

	if response.Error != nil {
		return fmt.Errorf(
			"rpc %d: %s",
			response.Error.Code,
			response.Error.Message,
		)
	}

	if len(response.Result) == 0 {
		return fmt.Errorf(
			"native response has no result",
		)
	}

	// tools/call thường trả:
	//
	// {
	//   "success": true,
	//   ...
	// }
	//
	// Không assume result chỉ có duy nhất field success.
	var result struct {
		Success *bool `json:"success"`
		IsError bool  `json:"isError"`

		ContentItems []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"contentItems"`

		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(
		response.Result,
		&result,
	); err != nil {
		return fmt.Errorf(
			"decode native tool result: %w",
			err,
		)
	}

	if result.IsError {
		return fmt.Errorf(
			"Desktop returned isError=true: %s",
			nativeResultText(
				result.ContentItems,
				result.Content,
			),
		)
	}

	if result.Success != nil &&
		!*result.Success {
		return fmt.Errorf(
			"Desktop returned success=false: %s",
			nativeResultText(
				result.ContentItems,
				result.Content,
			),
		)
	}

	return nil
}

func nativeResultText(
	contentItems []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	},
	content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	},
) string {
	var parts []string

	for _, item := range contentItems {
		if strings.TrimSpace(item.Text) == "" {
			continue
		}

		parts = append(
			parts,
			item.Text,
		)
	}

	for _, item := range content {
		if strings.TrimSpace(item.Text) == "" {
			continue
		}

		parts = append(
			parts,
			item.Text,
		)
	}

	return truncateString(
		strings.Join(
			parts,
			"\n",
		),
		2000,
	)
}

func resolveNativePipe() (
	pipe string,
	source string,
	envHasPipe bool,
) {
	// Case 1:
	// Desktop truyền trực tiếp native pipe vào env
	// của custom MCP.
	if raw := strings.TrimSpace(
		os.Getenv(
			"CODEX_APP_TOOLS_PIPE_PATH",
		),
	); raw != "" {
		envHasPipe = true

		if p := normalizeLocalPipe(
			raw,
		); p != "" {
			return p,
				"CODEX_APP_TOOLS_PIPE_PATH",
				true
		}
	}

	// Manual debug override.
	if raw := strings.TrimSpace(
		os.Getenv(
			"CDQG_DESKTOP_NATIVE_PIPE",
		),
	); raw != "" {
		if p := normalizeLocalPipe(
			raw,
		); p != "" {
			return p,
				"CDQG_DESKTOP_NATIVE_PIPE",
				envHasPipe
		}
	}

	// Case 2:
	// Một số Codex Desktop build chỉ truyền pipe
	// vào bundled codex_app MCP.
	//
	// Custom MCP phải đọc exact config override
	// từ codex.exe app-server cha.
	if p := discoverParentPipe(); p != "" {
		return p,
			"parent_app_server_command_line",
			envHasPipe
	}

	return "",
		"none",
		envHasPipe
}

func discoverParentPipe() string {
	processes := parentProcesses(
		os.Getppid(),
		12,
	)

	candidates := map[string]struct{}{}

	for _, process := range processes {
		name := strings.ToLower(
			filepath.Base(
				process.Name,
			),
		)

		if name != "codex.exe" &&
			name != "codex" {
			continue
		}

		pipe := nativePipeFromCodexCommandLine(
			process.CommandLine,
		)

		if pipe == "" {
			continue
		}

		candidates[pipe] = struct{}{}
	}

	// Không đoán nếu có nhiều pipe khác nhau.
	if len(candidates) != 1 {
		return ""
	}

	for pipe := range candidates {
		return pipe
	}

	return ""
}

func nativePipeFromCodexCommandLine(
	commandLine string,
) string {
	commandLine = strings.TrimSpace(
		commandLine,
	)

	if commandLine == "" {
		return ""
	}

	if strings.ContainsAny(
		commandLine,
		"\r\n\x00",
	) {
		return ""
	}

	args, err := splitWindowsCommandLine(
		commandLine,
	)

	if err != nil ||
		len(args) == 0 {
		return ""
	}

	executable := strings.ToLower(
		filepath.Base(
			args[0],
		),
	)

	if executable != "codex.exe" &&
		executable != "codex" {
		return ""
	}

	appServer := false

	var overrides []string

	for i := 1; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "app-server":
			appServer = true

		case arg == "-c" ||
			arg == "--config":

			if i+1 >= len(args) {
				return ""
			}

			i++

			overrides = append(
				overrides,
				args[i],
			)

		case strings.HasPrefix(
			arg,
			"--config=",
		):
			overrides = append(
				overrides,
				strings.TrimPrefix(
					arg,
					"--config=",
				),
			)
		}
	}

	if !appServer {
		return ""
	}

	candidates := map[string]struct{}{}

	for _, override := range overrides {
		pipe := nativePipeFromConfigOverride(
			override,
		)

		if pipe == "" {
			continue
		}

		candidates[pipe] = struct{}{}
	}

	if len(candidates) != 1 {
		return ""
	}

	for pipe := range candidates {
		return pipe
	}

	return ""
}

func nativePipeFromConfigOverride(
	override string,
) string {
	override = strings.TrimSpace(
		override,
	)

	if override == "" {
		return ""
	}

	// Format phổ biến:
	//
	// mcp_servers.codex_app={
	//   command="...",
	//   env={
	//     CODEX_APP_TOOLS_PIPE_PATH='\\.\pipe\...'
	//   }
	// }

	match := codexAppOverrideRE.
		FindStringSubmatch(
			override,
		)

	if len(match) == 2 {
		return extractPipeFromCodexAppTable(
			match[1],
		)
	}

	// Một số build có thể override trực tiếp:
	//
	// mcp_servers.codex_app.env.CODEX_APP_TOOLS_PIPE_PATH="..."
	const directPrefix = "mcp_servers.codex_app.env.CODEX_APP_TOOLS_PIPE_PATH"

	lower := strings.ToLower(
		override,
	)

	if strings.HasPrefix(
		lower,
		strings.ToLower(directPrefix),
	) {
		index := strings.Index(
			override,
			"=",
		)

		if index < 0 {
			return ""
		}

		raw := strings.TrimSpace(
			override[index+1:],
		)

		value, ok := decodeTOMLString(
			raw,
		)

		if !ok {
			return ""
		}

		return normalizeLocalPipe(
			value,
		)
	}

	return ""
}

func extractPipeFromCodexAppTable(
	table string,
) string {
	matches := codexAppPipeRE.
		FindAllStringSubmatch(
			table,
			-1,
		)

	if len(matches) == 0 {
		return ""
	}

	candidates := map[string]struct{}{}

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		value, ok := decodeTOMLString(
			match[1],
		)

		if !ok {
			continue
		}

		pipe := normalizeLocalPipe(
			value,
		)

		if pipe == "" {
			continue
		}

		candidates[pipe] = struct{}{}
	}

	if len(candidates) != 1 {
		return ""
	}

	for pipe := range candidates {
		return pipe
	}

	return ""
}

func decodeTOMLString(
	raw string,
) (string, bool) {
	raw = strings.TrimSpace(
		raw,
	)

	if len(raw) < 2 {
		return "", false
	}

	// TOML literal string:
	//
	// '\\.\pipe\abc'
	if raw[0] == '\'' &&
		raw[len(raw)-1] == '\'' {
		return raw[1 : len(raw)-1],
			true
	}

	// TOML basic string:
	//
	// "\\\\.\\pipe\\abc"
	if raw[0] == '"' &&
		raw[len(raw)-1] == '"' {
		value, err := strconv.Unquote(
			raw,
		)

		if err != nil {
			return "", false
		}

		return value, true
	}

	return "", false
}

func normalizeLocalPipe(
	value string,
) string {
	value = strings.TrimSpace(
		value,
	)

	// Có thể nhận thêm quote khi dùng env override.
	value = strings.Trim(
		value,
		`"'`,
	)

	// Trường hợp text bị giữ double escaped.
	if strings.HasPrefix(
		value,
		`\\\\.\\pipe\\`,
	) {
		value = strings.ReplaceAll(
			value,
			`\\`,
			`\`,
		)
	}

	const prefix = `\\.\pipe\`

	if len(value) <= len(prefix) {
		return ""
	}

	if !strings.HasPrefix(
		strings.ToLower(value),
		strings.ToLower(prefix),
	) {
		return ""
	}

	// Chỉ local named pipe.
	// \\machine\pipe\... không được chấp nhận.
	if !strings.HasPrefix(
		value,
		`\\.\`,
	) {
		return ""
	}

	// Pipe name không nên có slash rỗng ở cuối.
	value = strings.TrimRight(
		value,
		`\`,
	)

	if len(value) <= len(prefix) {
		return ""
	}

	return value
}

func parentProcesses(
	startPID int,
	maxDepth int,
) []parentProcess {
	if startPID <= 0 ||
		maxDepth <= 0 {
		return nil
	}

	script := fmt.Sprintf(
		`$id=%d; `+
			`for($i=0; $i -lt %d -and $id -gt 0; $i++){ `+
			`$p=Get-CimInstance Win32_Process `+
			`-Filter ("ProcessId="+$id) `+
			`-ErrorAction SilentlyContinue; `+
			`if($null -eq $p){break}; `+
			`[pscustomobject]@{`+
			`ProcessId=[int]$p.ProcessId;`+
			`ParentProcessId=[int]$p.ParentProcessId;`+
			`Name=[string]$p.Name;`+
			`CommandLine=[string]$p.CommandLine`+
			`} | ConvertTo-Json -Compress; `+
			`$id=[int]$p.ParentProcessId `+
			`}`,
		startPID,
		maxDepth,
	)

	shell := "powershell.exe"

	// Ưu tiên pwsh nếu máy có.
	if path, err := exec.LookPath(
		"pwsh.exe",
	); err == nil {
		shell = path
	}

	cmd := exec.Command(
		shell,
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		script,
	)

	out, err := cmd.Output()

	if err != nil {
		return nil
	}

	var result []parentProcess

	scanner := bufio.NewScanner(
		strings.NewReader(
			string(out),
		),
	)

	scanner.Buffer(
		make([]byte, 16*1024),
		1024*1024,
	)

	for scanner.Scan() {
		line := strings.TrimSpace(
			scanner.Text(),
		)

		if line == "" {
			continue
		}

		var process parentProcess

		if err := json.Unmarshal(
			[]byte(line),
			&process,
		); err != nil {
			continue
		}

		result = append(
			result,
			process,
		)
	}

	return result
}

func splitWindowsCommandLine(
	commandLine string,
) ([]string, error) {
	ptr, err := syscall.UTF16PtrFromString(
		commandLine,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"encode Windows command line: %w",
			err,
		)
	}

	var argc int32

	argv, _, callErr :=
		commandLineToArgvW.Call(
			uintptr(
				unsafe.Pointer(ptr),
			),
			uintptr(
				unsafe.Pointer(&argc),
			),
		)

	if argv == 0 {
		return nil, fmt.Errorf(
			"CommandLineToArgvW failed: %v",
			callErr,
		)
	}

	defer localFree.Call(
		argv,
	)

	ptrs := unsafe.Slice(
		(**uint16)(
			unsafe.Pointer(argv),
		),
		int(argc),
	)

	result := make(
		[]string,
		0,
		int(argc),
	)

	for _, ptr := range ptrs {
		if ptr == nil {
			result = append(
				result,
				"",
			)

			continue
		}

		result = append(
			result,
			utf16PtrString(ptr),
		)
	}

	return result, nil
}

func utf16PtrString(
	ptr *uint16,
) string {
	if ptr == nil {
		return ""
	}

	var values []uint16

	base := uintptr(
		unsafe.Pointer(ptr),
	)

	for offset := uintptr(0); ; offset += 2 {
		value := *(*uint16)(
			unsafe.Pointer(
				base + offset,
			),
		)

		if value == 0 {
			break
		}

		values = append(
			values,
			value,
		)
	}

	return syscall.UTF16ToString(
		values,
	)
}

func uuidLike() string {
	var bytes [16]byte

	if _, err := rand.Read(
		bytes[:],
	); err != nil {
		// Không dùng làm security token.
		return fmt.Sprintf(
			"%d",
			time.Now().UnixNano(),
		)
	}

	// UUID v4 bits.
	bytes[6] =
		(bytes[6] & 0x0f) | 0x40

	bytes[8] =
		(bytes[8] & 0x3f) | 0x80

	encoded := hex.EncodeToString(
		bytes[:],
	)

	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		encoded[0:8],
		encoded[8:12],
		encoded[12:16],
		encoded[16:20],
		encoded[20:32],
	)
}

func truncateString(
	value string,
	max int,
) string {
	if max <= 0 {
		return ""
	}

	if len(value) <= max {
		return value
	}

	return value[:max]
}
