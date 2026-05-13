"""Install, uninstall, and manage CLIs — dispatches to pip or npm based on source."""

import json
import shlex
import shutil
import subprocess
import sys
from pathlib import Path

from cli_hub.registry import get_cli

INSTALLED_FILE = Path.home() / ".cli-hub" / "installed.json"


def _load_installed():
    if INSTALLED_FILE.exists():
        try:
            return json.loads(INSTALLED_FILE.read_text())
        except json.JSONDecodeError:
            pass
    return {}


def _save_installed(data):
    INSTALLED_FILE.parent.mkdir(parents=True, exist_ok=True)
    INSTALLED_FILE.write_text(json.dumps(data, indent=2))


def _find_npm():
    """Find npm executable. Returns path or None."""
    return shutil.which("npm")


def _find_uv():
    """Find uv executable. Returns path or None."""
    return shutil.which("uv")


_UV_INSTALL_HINT = (
    "uv is not installed. Install it first:\n"
    "  macOS / Linux: curl -LsSf https://astral.sh/uv/install.sh | sh\n"
    "  Windows:       powershell -ExecutionPolicy ByPass -c \"irm https://astral.sh/uv/install.ps1 | iex\"\n"
    "  pip:           pip install uv\n"
    "  brew:          brew install uv\n"
    "  See also:      https://docs.astral.sh/uv/getting-started/installation/"
)


_SHELL_METACHARACTERS = ("|", "&&", "||", ";", "$(", "`")


def _run_command(cmd):
    """Run a command string.

    Uses shell=True when the command contains shell operators (pipes, &&, etc.)
    so that script-type installs like ``curl … | bash`` work correctly.
    Commands come from the trusted registry, not from user input.
    """
    use_shell = any(c in cmd for c in _SHELL_METACHARACTERS)
    try:
        return subprocess.run(
            cmd if use_shell else shlex.split(cmd),
            capture_output=True,
            text=True,
            shell=use_shell,
        )
    except FileNotFoundError as exc:
        missing = exc.filename or shlex.split(cmd)[0]
        return subprocess.CompletedProcess(
            args=cmd,
            returncode=127,
            stdout="",
            stderr=f"Command not found: {missing}",
        )


def _command_exists(cmd):
    """Check whether the executable for a command string exists on PATH."""
    try:
        parts = shlex.split(cmd)
    except ValueError:
        return False
    if not parts:
        return False
    return shutil.which(parts[0]) is not None


def _install_strategy(cli):
    """Return the install strategy for a CLI entry."""
    strategy = cli.get("install_strategy")
    if strategy:
        return strategy
    if cli.get("_source", "harness") == "harness":
        return "pip"
    if cli.get("npm_package") or cli.get("package_manager") == "npm":
        return "npm"
    if cli.get("package_manager") == "uv":
        return "uv"
    if cli.get("package_manager") == "bundled":
        return "bundled"
    return "command"


def _generic_install(cli):
    install_cmd = cli.get("install_cmd")
    if not install_cmd:
        return False, f"No install command is defined for {cli['display_name']}."
    result = _run_command(install_cmd)
    if result.returncode == 0:
        return True, f"Installed {cli['display_name']} ({cli['entry_point']})"
    return False, f"Install failed:\n{result.stderr or result.stdout}"


def _generic_uninstall(cli):
    uninstall_cmd = cli.get("uninstall_cmd")
    if not uninstall_cmd:
        note = cli.get("uninstall_notes") or f"No uninstall command is defined for {cli['display_name']}."
        return False, note
    result = _run_command(uninstall_cmd)
    if result.returncode == 0:
        return True, f"Uninstalled {cli['display_name']}"
    return False, f"Uninstall failed:\n{result.stderr or result.stdout}"


def _generic_update(cli):
    update_cmd = cli.get("update_cmd")
    if not update_cmd:
        note = cli.get("update_notes") or f"No update command is defined for {cli['display_name']}."
        return False, note
    result = _run_command(update_cmd)
    if result.returncode == 0:
        return True, f"Updated {cli['display_name']}"
    return False, f"Update failed:\n{result.stderr or result.stdout}"


def _bundled_install(cli):
    detect_cmd = cli.get("detect_cmd") or cli.get("entry_point")
    if detect_cmd and _command_exists(detect_cmd):
        return True, f"{cli['display_name']} is already available ({cli['entry_point']})"
    note = cli.get("install_notes") or (
        f"{cli['display_name']} is bundled with its parent app. "
        "Install or enable it in the upstream app first, then run this command again."
    )
    return False, note


def _bundled_uninstall(cli):
    note = cli.get("uninstall_notes") or (
        f"{cli['display_name']} is bundled with its parent app. "
        "Disable it in the upstream app or uninstall the parent app manually."
    )
    return False, note


def _bundled_update(cli):
    note = cli.get("update_notes") or (
        f"{cli['display_name']} is bundled with its parent app. "
        "Update the parent app to update this CLI."
    )
    return False, note


# ── pip operations (harness CLIs) ──


def _pip_install(cli):
    install_cmd = cli["install_cmd"]
    result = subprocess.run(
        [sys.executable, "-m", "pip", "install"] + install_cmd.replace("pip install ", "").split(),
        capture_output=True, text=True
    )
    if result.returncode == 0:
        return True, f"Installed {cli['display_name']} ({cli['entry_point']})"
    return False, f"pip install failed:\n{result.stderr}"


def _pip_uninstall(cli):
    pkg_name = f"cli-anything-{cli['name']}"
    result = subprocess.run(
        [sys.executable, "-m", "pip", "uninstall", "-y", pkg_name],
        capture_output=True, text=True
    )
    if result.returncode == 0:
        return True, f"Uninstalled {cli['display_name']}"
    return False, f"pip uninstall failed:\n{result.stderr}"


def _pip_update(cli):
    install_cmd = cli["install_cmd"]
    result = subprocess.run(
        [sys.executable, "-m", "pip", "install", "--upgrade", "--force-reinstall"]
        + install_cmd.replace("pip install ", "").split(),
        capture_output=True, text=True
    )
    if result.returncode == 0:
        return True, f"Updated {cli['display_name']} to {cli['version']}"
    return False, f"Update failed:\n{result.stderr}"


# ── uv operations (public CLIs) ──
