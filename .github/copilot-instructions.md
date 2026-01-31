- Do not guess the repository/project root.
- Before assuming any paths, first confirm the current working directory and environment by running an appropriate “where am I?” command via `#tool:execute/runInTerminal`.
  - Examples (choose what fits the shell/environment):
    - PowerShell: `Get-Location`
    - cmd.exe: `cd`
    - bash/zsh: `pwd`

- To locate the project/repo root, prefer reliable discovery over assumptions:
  - If this looks like a Git repo, try `git rev-parse --show-toplevel`.
  - Otherwise, enumerate nearby files/folders to orient yourself (e.g., `ls` / `dir` / `Get-ChildItem`) and search for typical root markers (e.g., `.git`, `README*`, `package.json`, `pyproject.toml`, `go.mod`, etc.).

- If you need to fetch a file but you are unsure of its path:
  1) First try to access it by filename only (no directory), e.g. `README.md`, to see whether the environment/tools can resolve it from the current directory.
  2) If that fails, use `#tool:search` to find the correct file path(s).
  3) The preferred flow is: use `#tool:search` to identify the right file → then open/read it by running an environment-appropriate command via `#tool:execute/runInTerminal` (e.g., `type` / `cat` / `Get-Content`), rather than inventing paths.

- Use path formats that match the detected environment (Windows vs POSIX). When in doubt, prefer absolute paths after you have verified the root/cwd.

- If a command fails due to path/cwd issues, do not retry blindly. Stop, re-check the current directory and discovered root, then proceed with a corrected path.
