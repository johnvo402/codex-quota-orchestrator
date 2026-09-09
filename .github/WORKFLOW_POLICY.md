# Workflow policy

Development happens on `dev`.

- Direct commits to `dev` do not run GitHub Actions.
- Pull requests targeting `main` run CI.
- Merges/pushes to `main` run CI.
- `Tag Release` runs only from `main` and creates the version in `.release-version` when that tag does not already exist.
- A newly created tag dispatches the `Release` workflow, which only accepts tags reachable from `main`.

For a release, bump `.release-version` on `dev`, include that change in the PR to `main`, let PR CI pass, then merge.
