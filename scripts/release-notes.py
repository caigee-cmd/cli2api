#!/usr/bin/env python3
"""Render bilingual GitHub release notes from changelog fragments."""

from __future__ import annotations

import argparse
import datetime as dt
import re
import subprocess
import sys
from pathlib import Path

H2_RE = re.compile(r"^##\s+(.+?)\s*$")
H3_RE = re.compile(r"^###\s+(.+?)\s*$")
BULLET_RE = re.compile(r"^[-*]\s+\S")
VERSION_RE = re.compile(r"^v?(\d+\.\d+\.\d+)(?:\s+-\s+\d{4}-\d{2}-\d{2})?$", re.IGNORECASE)
FRAGMENT_NAME_RE = re.compile(r"^[a-z0-9][a-z0-9._-]*\.md$")

LANG_ALIASES = {
    "english": "English",
    "en": "English",
    "en-us": "English",
    "中文": "中文",
    "chinese": "中文",
    "zh": "中文",
    "zh-cn": "中文",
    "zh-hans": "中文",
}

DEFAULT_CHANGELOG = Path("CHANGELOG.md")
DEFAULT_UNRELEASED = Path("changelog/unreleased")


class ChangelogError(Exception):
    pass


def normalize_version(value: str) -> str:
    text = value.strip()
    if text.lower() == "unreleased":
        return "unreleased"
    text = text[1:] if text[:1] in "vV" else text
    match = re.fullmatch(r"\d+\.\d+\.\d+", text)
    if not match:
        raise ChangelogError(f"invalid version {value!r}")
    return text


def parse_h2(text: str) -> list[tuple[str, list[str]]]:
    sections: list[tuple[str, list[str]]] = []
    title: str | None = None
    body: list[str] = []
    for line in text.splitlines():
        match = H2_RE.match(line)
        if match:
            if title is not None:
                sections.append((title, body))
            title = match.group(1).strip()
            body = []
            continue
        if title is not None:
            body.append(line)
    if title is not None:
        sections.append((title, body))
    return sections


def parse_h3(lines: list[str]) -> dict[str, list[str]]:
    sections: dict[str, list[str]] = {}
    title: str | None = None
    body: list[str] = []
    preamble: list[str] = []
    for line in lines:
        match = H3_RE.match(line)
        if match:
            if title is not None:
                sections[title] = body
            title = match.group(1).strip()
            body = []
            continue
        if title is None:
            preamble.append(line)
        else:
            body.append(line)
    if title is not None:
        sections[title] = body
    leftover = [line.strip() for line in preamble if line.strip()]
    if leftover:
        raise ChangelogError("changelog section must start with ### English and ### 中文")
    return sections


def section_heading_version(title: str) -> str | None:
    if title.strip().lower() == "unreleased":
        return "unreleased"
    match = VERSION_RE.match(title.strip())
    if not match:
        return None
    return match.group(1)


def find_section(sections: list[tuple[str, list[str]]], version: str) -> tuple[str, list[str]]:
    wanted = normalize_version(version)
    for title, body in sections:
        if section_heading_version(title) == wanted:
            return title, body
    label = "Unreleased" if wanted == "unreleased" else wanted
    raise ChangelogError(f"CHANGELOG.md has no {label} section")


def language_blocks(body: list[str]) -> dict[str, str]:
    blocks: dict[str, str] = {}
    for title, lines in parse_h3(body).items():
        lang = LANG_ALIASES.get(title.strip().lower())
        if lang is None:
            raise ChangelogError(f"unsupported changelog language heading {title!r}")
        if lang in blocks:
            raise ChangelogError(f"duplicate {lang} section")
        blocks[lang] = "\n".join(lines).strip()
    extra = set(blocks) - {"English", "中文"}
    if extra:
        raise ChangelogError(f"unexpected language sections: {sorted(extra)}")
    return blocks


def has_bullets(text: str) -> bool:
    return any(BULLET_RE.match(line.strip()) for line in text.splitlines())


def require_notes(blocks: dict[str, str], version_label: str) -> dict[str, str]:
    notes: dict[str, str] = {}
    for lang in ("English", "中文"):
        text = blocks.get(lang, "").strip()
        if lang not in blocks:
            raise ChangelogError(f"{version_label} is missing a {lang} heading")
        if not has_bullets(text):
            raise ChangelogError(f"{version_label} {lang} changelog has no bullet items")
        notes[lang] = text
    english_count = len(bullets(notes["English"]))
    chinese_count = len(bullets(notes["中文"]))
    if english_count != chinese_count:
        raise ChangelogError(
            f"{version_label} English has {english_count} bullets, 中文 has {chinese_count}"
        )
    return notes


def render_notes(notes: dict[str, str]) -> str:
    return (
        "\n\n".join(
            [
                "## English",
                notes["English"].strip(),
                "## 中文",
                notes["中文"].strip(),
            ]
        ).strip()
        + "\n"
    )


def parse_rendered_notes(text: str) -> dict[str, str]:
    blocks = {
        LANG_ALIASES.get(title.strip().lower(), title): "\n".join(body).strip()
        for title, body in parse_h2(text)
    }
    return require_notes(blocks, "release notes")


def bullets(text: str) -> list[str]:
    items: list[str] = []
    for line in text.splitlines():
        stripped = line.strip()
        if BULLET_RE.match(stripped):
            items.append(re.sub(r"^[-*]\s+", "", stripped))
    return items


CHANGELOG_INTRO = (
    "# Changelog\n"
    "\n"
    "Published user-facing notes for GitHub Releases and the console update page.\n"
    "Write upcoming notes as bilingual files in `changelog/unreleased/`.\n"
)


def render_version_section(title: str, notes: dict[str, str]) -> str:
    chunks = [f"## {title}", ""]
    for lang in ("English", "中文"):
        chunks.append(f"### {lang}")
        chunks.append("")
        chunks.append(notes[lang].strip())
        chunks.append("")
    return "\n".join(chunks)


def insert_version_section(original: str, title: str, notes: dict[str, str]) -> str:
    section = render_version_section(title, notes).rstrip() + "\n"
    match = re.search(r"^##\s+", original, re.MULTILINE)
    if match is None:
        return original.rstrip() + "\n\n" + section
    start = match.start()
    return original[:start].rstrip() + "\n\n" + section + "\n" + original[start:]


def load_changelog(path: Path) -> list[tuple[str, list[str]]]:
    if not path.is_file():
        raise ChangelogError(f"missing {path}")
    return parse_h2(path.read_text(encoding="utf-8"))


def notes_from_section(body: list[str], label: str) -> dict[str, str]:
    blocks = language_blocks(body)
    return require_notes(blocks, label)


def fragment_paths(unreleased_dir: Path) -> list[Path]:
    if not unreleased_dir.exists():
        return []
    if not unreleased_dir.is_dir():
        raise ChangelogError(f"{unreleased_dir} is not a directory")
    paths: list[Path] = []
    for path in sorted(unreleased_dir.iterdir(), key=lambda item: item.name):
        if path.name.lower() in {".gitkeep", "readme.md"} or path.name.startswith("."):
            continue
        if not path.is_file():
            raise ChangelogError(f"unexpected path in unreleased changelog: {path.name}")
        if not FRAGMENT_NAME_RE.fullmatch(path.name):
            raise ChangelogError(
                f"invalid changelog fragment name {path.name!r}; "
                "use lowercase ASCII kebab-case like check-in-scheduler.md"
            )
        paths.append(path)
    return paths


def load_fragment(path: Path) -> dict[str, str]:
    text = path.read_text(encoding="utf-8")
    if H2_RE.match(text.lstrip().splitlines()[0] if text.strip() else ""):
        raise ChangelogError(f"{path.name} must start with ### English and ### 中文, not a ## heading")
    try:
        return require_notes(language_blocks(text.splitlines()), path.name)
    except ChangelogError as exc:
        raise ChangelogError(f"{path.name}: {exc}") from exc


def load_fragments(unreleased_dir: Path) -> list[tuple[str, dict[str, str]]]:
    loaded: list[tuple[str, dict[str, str]]] = []
    for path in fragment_paths(unreleased_dir):
        loaded.append((path.name, load_fragment(path)))
    return loaded


def merge_notes(items: list[dict[str, str]]) -> dict[str, str]:
    merged: dict[str, str] = {}
    for lang in ("English", "中文"):
        parts = [notes[lang].strip() for notes in items if notes[lang].strip()]
        merged[lang] = "\n".join(parts).strip()
    return merged


def extract_unreleased(unreleased_dir: Path) -> str:
    fragments = load_fragments(unreleased_dir)
    if not fragments:
        raise ChangelogError("changelog/unreleased/ has no bilingual fragment files")
    return render_notes(merge_notes([notes for _, notes in fragments]))


def extract(changelog: Path, unreleased_dir: Path, version: str) -> str:
    wanted = normalize_version(version)
    if wanted == "unreleased":
        return extract_unreleased(unreleased_dir)
    title, body = find_section(load_changelog(changelog), wanted)
    return render_notes(notes_from_section(body, title))


def extract_for_release(changelog: Path, unreleased_dir: Path, version: str) -> str:
    fragments = load_fragments(unreleased_dir)
    if fragments:
        return render_notes(merge_notes([notes for _, notes in fragments]))

    wanted = normalize_version(version)
    try:
        title, body = find_section(load_changelog(changelog), wanted)
    except ChangelogError as exc:
        raise ChangelogError(
            f"add bilingual files in changelog/unreleased/ before releasing {wanted}"
        ) from exc
    return render_notes(notes_from_section(body, title))


def validate_changelog(path: Path) -> list[tuple[str, dict[str, str]]]:
    sections = load_changelog(path)
    if not sections:
        raise ChangelogError("CHANGELOG.md has no version sections")

    versions: list[tuple[str, dict[str, str]]] = []
    seen: set[str] = set()
    for title, body in sections:
        heading = section_heading_version(title)
        if heading is None:
            raise ChangelogError(f"unsupported changelog heading {title!r}")
        if heading == "unreleased":
            raise ChangelogError(
                "CHANGELOG.md must not contain ## Unreleased; "
                "write upcoming notes in changelog/unreleased/"
            )
        if heading in seen:
            raise ChangelogError(f"duplicate changelog section {title!r}")
        seen.add(heading)
        versions.append((title, notes_from_section(body, heading)))
    return versions


def validate(changelog: Path, unreleased_dir: Path) -> None:
    validate_changelog(changelog)
    load_fragments(unreleased_dir)


def fragment_names_at_sha(sha: str) -> list[str]:
    result = subprocess.run(
        ["git", "ls-tree", "-r", "--name-only", sha, "--", "changelog/unreleased"],
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip() or f"exit {result.returncode}"
        raise ChangelogError(f"could not list changelog fragments at {sha}: {detail}")
    names: list[str] = []
    for line in result.stdout.splitlines():
        path = Path(line.strip())
        if path.parent != Path("changelog/unreleased"):
            continue
        if path.name.lower() in {".gitkeep", "readme.md"}:
            continue
        if path.suffix == ".md" and FRAGMENT_NAME_RE.fullmatch(path.name):
            names.append(path.name)
    return names


def consume(
    changelog: Path,
    unreleased_dir: Path,
    version: str,
    date: str,
    notes_text: str,
    fragment_names: list[str],
) -> bool:
    version = normalize_version(version)
    if version == "unreleased":
        raise ChangelogError("cannot archive Unreleased")
    if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", date):
        raise ChangelogError(f"invalid archive date {date!r}")

    published = parse_rendered_notes(notes_text)
    original = changelog.read_text(encoding="utf-8")
    found = False
    for title, notes in validate_changelog(changelog):
        existing = section_heading_version(title)
        if existing != version:
            continue
        if notes != published:
            raise ChangelogError(f"{version} already exists with different notes")
        found = True
        break

    rewritten = original
    if not found:
        rewritten = insert_version_section(original, f"{version} - {date}", published)
    if not rewritten.startswith("# Changelog"):
        rewritten = CHANGELOG_INTRO + "\n" + rewritten.lstrip()

    changed = rewritten != original
    if changed:
        changelog.write_text(rewritten, encoding="utf-8")

    unreleased_dir.mkdir(parents=True, exist_ok=True)
    for name in fragment_names:
        path = unreleased_dir / name
        if path.is_file():
            path.unlink()
            changed = True
    return changed


def self_test() -> None:
    sample_archive = """# Changelog

Published user-facing notes.

## 0.1.0 - 2026-08-22

### English

- First release

### 中文

- 首次发布
"""
    with _temp_tree() as (root, changelog, unreleased):
        changelog.write_text(sample_archive, encoding="utf-8")
        (unreleased / "host-updater.md").write_text(
            """### English

- Add host updater
- Keep SQLite snapshots

### 中文

- 增加本机更新器
- 保留 SQLite 快照
""",
            encoding="utf-8",
        )
        validate(changelog, unreleased)
        notes = extract_for_release(changelog, unreleased, "v0.1.1")
        assert "## English" in notes and "## 中文" in notes
        assert "- Add host updater" in notes
        assert "- 增加本机更新器" in notes

        (unreleased / "README.md").write_text("# ignored\n", encoding="utf-8")
        assert all(name != "README.md" for name, _ in load_fragments(unreleased))
        leftover = unreleased / "later-work.md"
        leftover.write_text(
            """### English

- Keep leftover work

### 中文

- 保留未发布改动
""",
            encoding="utf-8",
        )
        assert consume(
            changelog,
            unreleased,
            "v0.1.1",
            "2026-08-24",
            notes,
            ["host-updater.md"],
        )
        frozen = changelog.read_text(encoding="utf-8")
        assert "## 0.1.1 - 2026-08-24" in frozen
        assert "- Add host updater" in frozen
        assert "\n\n## 0.1.0 - 2026-08-22\n" in frozen
        assert not (unreleased / "host-updater.md").exists()
        assert leftover.is_file()
        assert (unreleased / "README.md").is_file()
        remaining = extract_unreleased(unreleased)
        assert "Keep leftover work" in remaining
        assert "Add host updater" not in remaining
        assert consume(changelog, unreleased, "0.1.1", "2026-08-24", notes, ["host-updater.md"]) is False

    with _temp_tree() as (root, changelog, unreleased):
        changelog.write_text(sample_archive, encoding="utf-8")
        fallback = extract_for_release(changelog, unreleased, "0.1.0")
        assert "- First release" in fallback
        try:
            extract_for_release(changelog, unreleased, "0.1.1")
        except ChangelogError as exc:
            assert "unreleased" in str(exc)
        else:
            raise AssertionError("expected missing fragments to fail")

    with _temp_tree() as (root, changelog, unreleased):
        changelog.write_text(sample_archive, encoding="utf-8")
        (unreleased / "mismatch.md").write_text(
            """### English

- Add host updater

### 中文

- 增加本机更新器
- 额外一行
""",
            encoding="utf-8",
        )
        try:
            extract_for_release(changelog, unreleased, "0.1.1")
        except ChangelogError as exc:
            assert "bullet" in str(exc).lower()
        else:
            raise AssertionError("expected mismatched bullet counts to fail")

    with _temp_tree() as (root, changelog, unreleased):
        changelog.write_text(
            sample_archive.replace("## 0.1.0", "## Unreleased\n\n### English\n\n### 中文\n\n## 0.1.0"),
            encoding="utf-8",
        )
        try:
            validate(changelog, unreleased)
        except ChangelogError as exc:
            assert "Unreleased" in str(exc)
        else:
            raise AssertionError("expected leftover Unreleased heading to fail")

    with _temp_tree() as (root, changelog, unreleased):
        changelog.write_text(sample_archive, encoding="utf-8")
        (unreleased / "Bad Name.md").write_text(
            """### English

- Bad name

### 中文

- 错误文件名
""",
            encoding="utf-8",
        )
        try:
            load_fragments(unreleased)
        except ChangelogError as exc:
            assert "invalid changelog fragment name" in str(exc)
        else:
            raise AssertionError("expected invalid fragment names to fail")

    from io import StringIO

    captured = StringIO()
    old_stderr = sys.stderr
    sys.stderr = captured
    try:
        assert main(["freeze"]) == 1
    finally:
        sys.stderr = old_stderr
    assert "freeze was removed" in captured.getvalue()


def _temp_tree():
    import tempfile
    from contextlib import contextmanager

    @contextmanager
    def inner():
        with tempfile.TemporaryDirectory(prefix="cli2api-changelog-") as raw:
            root = Path(raw)
            changelog = root / "CHANGELOG.md"
            unreleased = root / "changelog" / "unreleased"
            unreleased.mkdir(parents=True)
            yield root, changelog, unreleased

    return inner()


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--changelog", default=str(DEFAULT_CHANGELOG), help="path to CHANGELOG.md")
    parser.add_argument(
        "--unreleased",
        default=str(DEFAULT_UNRELEASED),
        help="directory of unpublished bilingual fragments",
    )
    sub = parser.add_subparsers(dest="command", required=True)

    extract_cmd = sub.add_parser("extract", help="print notes for unreleased fragments or one archived version")
    extract_cmd.add_argument("version", help="unreleased or x.y.z")

    release_cmd = sub.add_parser("extract-for-release", help="print notes for the next GitHub release")
    release_cmd.add_argument("version", help="x.y.z being published")

    sub.add_parser("validate", help="validate changelog archive and unreleased fragments")
    sub.add_parser("self-test", help="run extractor checks")

    consume_cmd = sub.add_parser("consume", help="archive published notes and delete shipped fragments")
    consume_cmd.add_argument("version", help="x.y.z being published")
    consume_cmd.add_argument("--date", default=dt.date.today().isoformat())
    consume_cmd.add_argument("--notes-file", required=True, help="rendered notes that were published")
    consume_cmd.add_argument(
        "--release-sha",
        required=True,
        help="git SHA whose changelog/unreleased files shipped in this release",
    )
    sub.add_parser("freeze", help="removed; the release workflow archives via consume")
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    changelog = Path(args.changelog)
    unreleased = Path(args.unreleased)
    try:
        if args.command == "self-test":
            self_test()
            return 0
        if args.command == "validate":
            validate(changelog, unreleased)
            return 0
        if args.command == "extract":
            sys.stdout.write(extract(changelog, unreleased, args.version))
            return 0
        if args.command == "extract-for-release":
            sys.stdout.write(extract_for_release(changelog, unreleased, args.version))
            return 0
        if args.command == "freeze":
            raise ChangelogError(
                "freeze was removed; write notes in changelog/unreleased/ and let "
                "release.yml open the archive PR via consume"
            )
        notes_text = Path(args.notes_file).read_text(encoding="utf-8")
        consume(
            changelog,
            unreleased,
            args.version,
            args.date,
            notes_text,
            fragment_names_at_sha(args.release_sha),
        )
        return 0
    except ChangelogError as exc:
        print(exc, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
