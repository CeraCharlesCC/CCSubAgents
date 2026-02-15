- Do not guess the repository/project root.
- Before assuming any paths, first confirm the current working directory and environment by running an appropriate “where am I?” command via `#tool:execute/runInTerminal`.
  - Examples (choose what fits the shell/environment):
    - PowerShell: `Get-Location`
    - cmd.exe: `cd`
    - bash/zsh: `pwd`

- To locate the project/repo root, prefer reliable discovery over assumptions:
  - If this looks like a Git repo, try `git rev-parse --show-toplevel`.
  - Otherwise, enumerate nearby files/folders to orient yourself (e.g., `ls` / `dir` / `Get-ChildItem`) and search for typical root markers (e.g., `.git`, `README*`, `package.json`, `pyproject.toml`, `go.mod`, etc.).