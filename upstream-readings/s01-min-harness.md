# Agent Harness: GUI-to-CLI for Open Source Software

## Purpose

This harness provides a standard operating procedure (SOP) and toolkit for coding
agents (Claude Code, Codex, etc.) to build powerful, stateful CLI interfaces for
open-source GUI applications. The goal: let AI agents operate software that was
designed for humans, without needing a display or mouse.

## General SOP: Turning Any GUI App into an Agent-Usable CLI

### Phase 1: Codebase Analysis

1. **Identify the backend engine** — Most GUI apps separate presentation from logic.
   Find the core library/framework (e.g., MLT for Shotcut, ImageMagick for GIMP).
2. **Map GUI actions to API calls** — Every button click, drag, and menu item
   corresponds to a function call. Catalog these mappings.
3. **Identify the data model** — What file formats does it use? How is project state
   represented? (XML, JSON, binary, database?)
4. **Find existing CLI tools** — Many backends ship their own CLI (`melt`, `ffmpeg`,
   `convert`). These are building blocks.
5. **Catalog the command/undo system** — If the app has undo/redo, it likely uses a
   command pattern. These commands are your CLI operations.

### Phase 2: CLI Architecture Design

1. **Choose the interaction model**:
   - **Stateful REPL** for interactive sessions (agents that maintain context)
   - **Subcommand CLI** for one-shot operations (scripting, pipelines)
   - **Both** (recommended) — a CLI that works in both modes

2. **Define command groups** matching the app's logical domains:
   - Project management (new, open, save, close)
   - Core operations (the app's primary purpose)
   - Import/Export (file I/O, format conversion)
   - Configuration (settings, preferences, profiles)
   - Session/State management (undo, redo, history, status)

3. **Design the state model**:
   - What must persist between commands? (open project, cursor position, selection)
   - Where is state stored? (in-memory for REPL, file-based for CLI)
   - How does state serialize? (JSON session files)

4. **Plan the output format**:
   - Human-readable (tables, colors) for interactive use
   - Machine-readable (JSON) for agent consumption
   - Both, controlled by `--json` flag

### Phase 3: Implementation

1. **Start with the data layer** — XML/JSON manipulation of project files
2. **Add probe/info commands** — Let agents inspect before they modify
3. **Add mutation commands** — One command per logical operation
4. **Add the backend integration** — A `utils/<software>_backend.py` module that
   wraps the real software's CLI. This module handles:
   - Finding the software executable (`shutil.which()`)
   - Invoking it with proper arguments (`subprocess.run()`)
   - Error handling with clear install instructions if not found
   - Example (LibreOffice):
     ```python
     # utils/lo_backend.py
     def convert_odf_to(odf_path, output_format, output_path=None, overwrite=False):
         lo = find_libreoffice()  # raises RuntimeError with install instructions
         subprocess.run([lo, "--headless", "--convert-to", output_format, ...])
         return {"output": final_path, "format": output_format, "method": "libreoffice-headless"}
     ```
5. **Add rendering/export** — The export pipeline calls the backend module.
   Generate valid intermediate files, then invoke the real software for conversion.
6. **Add session management** — State persistence, undo/redo

   **Session file locking** — Use exclusive file locking for session JSON saves
   to prevent concurrent write corruption. See [`guides/session-locking.md`](guides/session-locking.md)
   for the `_locked_save_json` pattern (open `"r+"`, lock, then truncate inside the lock).
7. **Add the REPL with unified skin** — Interactive mode wrapping the subcommands.
   - Copy `repl_skin.py` from the plugin (`cli-anything-plugin/repl_skin.py`) into
     `utils/repl_skin.py` in your CLI package
   - Import and use `ReplSkin` for the REPL interface:
     ```python
     from cli_anything.<software>.utils.repl_skin import ReplSkin

     skin = ReplSkin("<software>", version="1.0.0")
     skin.print_banner()          # Branded startup box (prefers repo-root skills/, falls back to package)
     pt_session = skin.create_prompt_session()  # prompt_toolkit with history + styling
     line = skin.get_input(pt_session, project_name="my_project", modified=True)
     skin.help(commands_dict)     # Formatted help listing
     skin.success("Saved")        # ✓ green message
     skin.error("Not found")      # ✗ red message
     skin.warning("Unsaved")      # ⚠ yellow message
     skin.info("Processing...")   # ● blue message
     skin.status("Key", "value")  # Key-value status line
     skin.table(headers, rows)    # Formatted table
     skin.progress(3, 10, "...")  # Progress bar
     skin.print_goodbye()         # Styled exit message
     ```
   - ReplSkin prefers the repo-root canonical `skills/cli-anything-<software>/SKILL.md`
     when running inside this monorepo, and falls back to the packaged
     `cli_anything/<software>/skills/SKILL.md` copy when installed elsewhere.
     AI agents can read the skill file at the displayed absolute path.
   - Make REPL the default behavior: use `invoke_without_command=True` on the main
     Click group, and invoke the `repl` command when no subcommand is given:
     ```python
     @click.group(invoke_without_command=True)
     @click.pass_context
     def cli(ctx, ...):
         ...
         if ctx.invoked_subcommand is None:
             ctx.invoke(repl, project_path=None)
     ```
   - This ensures `cli-anything-<software>` with no arguments enters the REPL

### Phase 4: Test Planning (TEST.md - Part 1)

**BEFORE writing any test code**, create a `TEST.md` file in the
`agent-harness/cli_anything/<software>/tests/` directory. This file serves as your test plan and
MUST contain:

1. **Test Inventory Plan** — List planned test files and estimated test counts:
   - `test_core.py`: XX unit tests planned
   - `test_full_e2e.py`: XX E2E tests planned

2. **Unit Test Plan** — For each core module, describe what will be tested:
   - Module name (e.g., `project.py`)
   - Functions to test
   - Edge cases to cover (invalid inputs, boundary conditions, error handling)
   - Expected test count

3. **E2E Test Plan** — Describe the real-world scenarios to test:
   - What workflows will be simulated?
   - What real files will be generated/processed?
   - What output properties will be verified?
   - What format validations will be performed?

4. **Realistic Workflow Scenarios** — Detail each multi-step workflow:
   - **Workflow name**: Brief title
   - **Simulates**: What real-world task (e.g., "photo editing pipeline",
     "podcast production", "product render setup")
   - **Operations chained**: Step-by-step operations
   - **Verified**: What output properties will be checked

This planning document ensures comprehensive test coverage before writing code.

### Phase 5: Test Implementation

Now write the actual test code based on the TEST.md plan:

1. **Unit tests** (`test_core.py`) — Every core function tested in isolation with
   synthetic data. No external dependencies.
2. **E2E tests — intermediate files** (`test_full_e2e.py`) — Verify the project files
   your CLI generates are structurally correct (valid XML, correct ZIP structure, etc.)
3. **E2E tests — true backend** (`test_full_e2e.py`) — **MUST invoke the real software.**
   Create a project, export via the actual software backend, and verify the output:
   - File exists and size > 0
   - Correct format (PDF magic bytes `%PDF-`, DOCX/XLSX/PPTX is valid ZIP/OOXML, etc.)
   - Content verification where possible (CSV contains expected data, etc.)
   - **Print artifact paths** so users can manually inspect: `print(f"\n  PDF: {path} ({size:,} bytes)")`
   - **No graceful degradation** — if the software isn't installed, tests fail, not skip
4. **Output verification** — **Don't trust that export works just because it exits
   successfully.** Verify outputs programmatically:
   - Magic bytes / file format validation
   - ZIP structure for OOXML formats (DOCX, XLSX, PPTX)
   - Pixel-level analysis for video/images (probe frames, compare brightness)
   - Audio analysis (RMS levels, spectral comparison)
   - Duration/format checks against expected values
5. **CLI subprocess tests** — Test the installed CLI command as a real user/agent would.
   The subprocess tests MUST also produce real final output (not just ODF intermediate).
   Use the `_resolve_cli` helper to run the installed `cli-anything-<software>` command:
   ```python
   def _resolve_cli(name):
       """Resolve installed CLI command; falls back to python -m for dev.

       Set env CLI_ANYTHING_FORCE_INSTALLED=1 to require the installed command.
       """
       import shutil
       force = os.environ.get("CLI_ANYTHING_FORCE_INSTALLED", "").strip() == "1"
       path = shutil.which(name)
       if path:
           print(f"[_resolve_cli] Using installed command: {path}")
           return [path]
       if force:
           raise RuntimeError(f"{name} not found in PATH. Install with: pip install -e .")
       module = name.replace("cli-anything-", "cli_anything.") + "." + name.split("-")[-1] + "_cli"
       print(f"[_resolve_cli] Falling back to: {sys.executable} -m {module}")
       return [sys.executable, "-m", module]


   class TestCLISubprocess:
       CLI_BASE = _resolve_cli("cli-anything-<software>")

       def _run(self, args, check=True):
           return subprocess.run(
               self.CLI_BASE + args,
               capture_output=True, text=True,
               check=check,
           )

       def test_help(self):
           result = self._run(["--help"])
           assert result.returncode == 0

       def test_project_new_json(self, tmp_dir):
