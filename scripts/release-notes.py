#!/usr/bin/env python3
"""Render bilingual GitHub release notes from CHANGELOG.md."""

from __future__ import annotations

import argparse
import datetime as dt
import re
import sys
from pathlib import Path

H2_RE = re.compile(r"^##\s+(.+?)\s*$")
H3_RE = re.compile(r"^###\s+(.+?)\s*$")
BULLET_RE = re.compile(r"^[-*]\s+\S")
VERSION_RE = re.compile(r"^v?(\d+\.\d+\.\d+)(?:\s+-\s+\d{4}-\d{2}-\d{2})?$", re.IGNORECASE)

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


def optional_notes(blocks: dict[str, str], version_label: str) -> dict[str, str] | None:
    present = [lang for lang in ("English", "中文") if has_bullets(blocks.get(lang, ""))]
    if not present:
        return None
    return require_notes(blocks, version_label)


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


def drop_bullets(text: str, removed: set[str]) -> str:
    kept: list[str] = []
    for line in text.splitlines():
        stripped = line.strip()
        if BULLET_RE.match(stripped):
            item = re.sub(r"^[-*]\s+", "", stripped)
            if item in removed:
                continue
        kept.append(line.rstrip())
    return "\n".join(kept).strip()


def render_changelog(unreleased: dict[str, str], versions: list[tuple[str, dict[str, str]]]) -> str:
    chunks = [
        "# Changelog",
        "",
        "User-facing notes for GitHub Releases and the console update page.",
        "Write each change in both `### English` and `### 中文` under `## Unreleased`.",
        "",
        "## Unreleased",
        "",
    ]
    for lang in ("English", "中文"):
        chunks.append(f"### {lang}")
        chunks.append("")
        body = unreleased.get(lang, "").strip()
        if body:
            chunks.append(body)
            chunks.append("")
    for title, notes in versions:
        chunks.append(f"## {title}")
        chunks.append("")
        for lang in ("English", "中文"):
            chunks.append(f"### {lang}")
            chunks.append("")
            chunks.append(notes[lang].strip())
            chunks.append("")
    return "\n".join(chunks).rstrip() + "\n"


def load_changelog(path: Path) -> list[tuple[str, list[str]]]:
    if not path.is_file():
        raise ChangelogError(f"missing {path}")
    return parse_h2(path.read_text(encoding="utf-8"))


def notes_from_section(body: list[str], label: str, required: bool) -> dict[str, str] | None:
    blocks = language_blocks(body)
    for lang in ("English", "中文"):
        if lang not in blocks:
            raise ChangelogError(f"{label} is missing a {lang} heading")
    if required:
        return require_notes(blocks, label)
    return optional_notes(blocks, label)


def extract(path: Path, version: str) -> str:
    title, body = find_section(load_changelog(path), version)
    label = "Unreleased" if section_heading_version(title) == "unreleased" else title
    return render_notes(require_notes(language_blocks(body), label))


def extract_for_release(path: Path, version: str) -> str:
    sections = load_changelog(path)
    _, unreleased_body = find_section(sections, "unreleased")
    unreleased = notes_from_section(unreleased_body, "Unreleased", required=False)
    if unreleased:
        return render_notes(unreleased)

    try:
        title, body = find_section(sections, version)
    except ChangelogError as exc:
        raise ChangelogError(
            f"write bilingual notes in ## Unreleased before releasing {normalize_version(version)}"
        ) from exc
    return render_notes(require_notes(language_blocks(body), title))


def validate(path: Path) -> None:
    sections = load_changelog(path)
    if not sections:
        raise ChangelogError("CHANGELOG.md has no version sections")
    if section_heading_version(sections[0][0]) != "unreleased":
        raise ChangelogError("CHANGELOG.md must start with ## Unreleased")

    seen: set[str] = set()
    for title, body in sections:
        heading = section_heading_version(title)
        if heading is None:
            raise ChangelogError(f"unsupported changelog heading {title!r}")
        if heading in seen:
            raise ChangelogError(f"duplicate changelog section {title!r}")
        seen.add(heading)
        blocks = language_blocks(body)
        for lang in ("English", "中文"):
            if lang not in blocks:
                raise ChangelogError(f"{title} is missing a {lang} heading")
        if heading == "unreleased":
            leftover = optional_notes(blocks, title)
            if leftover is None and any(has_bullets(blocks[lang]) for lang in ("English", "中文")):
                raise ChangelogError("Unreleased English and 中文 must stay in sync")
            continue
        require_notes(blocks, heading)


def freeze(path: Path, version: str, date: str, notes_text: str | None) -> bool:
    version = normalize_version(version)
    if version == "unreleased":
        raise ChangelogError("cannot freeze Unreleased")
    if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", date):
        raise ChangelogError(f"invalid freeze date {date!r}")

    original = path.read_text(encoding="utf-8")
    sections = parse_h2(original)
    _, unreleased_body = find_section(sections, "unreleased")
    current_unreleased = language_blocks(unreleased_body)
    for lang in ("English", "中文"):
        current_unreleased.setdefault(lang, "")

    if notes_text:
        published = parse_rendered_notes(notes_text)
    else:
        published = optional_notes(current_unreleased, "Unreleased")
        if published is None:
            published = require_notes(
                language_blocks(find_section(sections, version)[1]),
                version,
            )

    heading = f"{version} - {date}"
    versions: list[tuple[str, dict[str, str]]] = []
    found = False
    for title, body in sections:
        existing = section_heading_version(title)
        if existing == "unreleased":
            continue
        notes = require_notes(language_blocks(body), title)
        if existing == version:
            if notes != published:
                raise ChangelogError(f"{version} already exists with different notes")
            versions.append((title, notes))
            found = True
        else:
            versions.append((title, notes))
    if not found:
        versions.insert(0, (heading, published))

    remaining = {
        lang: drop_bullets(current_unreleased.get(lang, ""), set(bullets(published[lang])))
        for lang in ("English", "中文")
    }
    rewritten = render_changelog(remaining, versions)
    if rewritten == original:
        return False
    path.write_text(rewritten, encoding="utf-8")
    return True


def self_test() -> None:
    sample = """# Changelog

## Unreleased

### English

- Add host updater
- Keep SQLite snapshots

### 中文

- 增加本机更新器
- 保留 SQLite 快照

## 0.1.0 - 2026-08-22

### English

- First release

### 中文

- 首次发布
"""
    notes = extract_for_release(_write_tmp(sample), "v0.1.1")
    assert "## English" in notes and "## 中文" in notes
    assert "- Add host updater" in notes
    assert "- 增加本机更新器" in notes

    tmp = _write_tmp(sample)
    validate(tmp)
    assert freeze(tmp, "v0.1.1", "2026-08-24", notes)
    frozen = tmp.read_text(encoding="utf-8")
    assert "## 0.1.1 - 2026-08-24" in frozen
    assert "- Add host updater" in frozen
    remaining = language_blocks(find_section(parse_h2(frozen), "unreleased")[1])
    assert "Add host updater" not in remaining["English"]
    assert freeze(tmp, "0.1.1", "2026-08-24", notes) is False

    empty_unreleased = sample.replace("- Add host updater\n- Keep SQLite snapshots\n", "").replace(
        "- 增加本机更新器\n- 保留 SQLite 快照\n", ""
    )
    fallback = extract_for_release(_write_tmp(empty_unreleased), "0.1.0")
    assert "- First release" in fallback
    assert "- 首次发布" in fallback

    try:
        extract_for_release(_write_tmp(empty_unreleased), "0.1.1")
    except ChangelogError as exc:
        assert "Unreleased" in str(exc)
    else:
        raise AssertionError("expected missing notes to fail")

    mismatched = sample.replace("- Keep SQLite snapshots\n", "")
    try:
        extract_for_release(_write_tmp(mismatched), "0.1.1")
    except ChangelogError as exc:
        assert "bullet" in str(exc).lower() or "bullets" in str(exc)
    else:
        raise AssertionError("expected mismatched bullet counts to fail")

    leftover = sample.replace(
        "- Keep SQLite snapshots\n",
        "- Keep SQLite snapshots\n- Keep leftover work\n",
    ).replace(
        "- 保留 SQLite 快照\n",
        "- 保留 SQLite 快照\n- 保留未发布改动\n",
    )
    leftover_path = _write_tmp(leftover)
    assert freeze(leftover_path, "0.1.2", "2026-08-24", notes)
    remaining = language_blocks(find_section(parse_h2(leftover_path.read_text(encoding="utf-8")), "unreleased")[1])
    assert "Keep leftover work" in remaining["English"]
    assert "保留未发布改动" in remaining["中文"]
    leftover_path.unlink(missing_ok=True)
    tmp.unlink(missing_ok=True)


def _write_tmp(text: str) -> Path:
    path = Path("/tmp/cli2api-changelog-test.md")
    path.write_text(text, encoding="utf-8")
    return path


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--changelog", default="CHANGELOG.md", help="path to CHANGELOG.md")
    sub = parser.add_subparsers(dest="command", required=True)

    extract_cmd = sub.add_parser("extract", help="print notes for one changelog section")
    extract_cmd.add_argument("version", help="unreleased or x.y.z")

    release_cmd = sub.add_parser("extract-for-release", help="print notes for the next GitHub release")
    release_cmd.add_argument("version", help="x.y.z being published")

    sub.add_parser("validate", help="validate changelog structure")
    sub.add_parser("self-test", help="run extractor checks")

    freeze_cmd = sub.add_parser("freeze", help="move published notes out of Unreleased")
    freeze_cmd.add_argument("version", help="x.y.z being published")
    freeze_cmd.add_argument("--date", default=dt.date.today().isoformat())
    freeze_cmd.add_argument("--notes-file", help="rendered notes to freeze; defaults to Unreleased")
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    path = Path(args.changelog)
    try:
        if args.command == "self-test":
            self_test()
            return 0
        if args.command == "validate":
            validate(path)
            return 0
        if args.command == "extract":
            sys.stdout.write(extract(path, args.version))
            return 0
        if args.command == "extract-for-release":
            sys.stdout.write(extract_for_release(path, args.version))
            return 0
        notes_text = Path(args.notes_file).read_text(encoding="utf-8") if args.notes_file else None
        freeze(path, args.version, args.date, notes_text)
        return 0
    except ChangelogError as exc:
        print(exc, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
