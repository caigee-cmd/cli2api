import type { Lang } from '@/i18n/messages'

export type InlineNode =
  | { type: 'text'; value: string }
  | { type: 'code'; value: string }
  | { type: 'strong'; value: string }
  | { type: 'em'; value: string }
  | { type: 'link'; href: string; value: string }

export type BlockNode =
  | { type: 'heading'; level: 2 | 3 | 4; text: string }
  | { type: 'list'; items: { text: string; depth: number }[] }
  | { type: 'paragraph'; text: string }
  | { type: 'code'; text: string }
  | { type: 'quote'; text: string }

const LANG_HEADING_RE = /^(#{1,3})\s+(english|en|中文|chinese|zh|zh-cn|zh-hans)\s*$/i
const INLINE_RE = /`([^`]+)`|\*\*([^*]+)\*\*|(?<!\*)\*([^*\n]+)\*(?!\*)|\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g
const LIST_RE = /^(\s*)(?:[-*]|\d+[.)])\s+(\S.*)$/
const ZH_TITLES = new Set(['中文', 'chinese', 'zh', 'zh-cn', 'zh-hans'])
const EN_TITLES = new Set(['english', 'en'])

function isWantedLang(title: string, lang: Lang) {
  const key = title.trim().toLowerCase()
  return lang === 'zh' ? ZH_TITLES.has(key) : EN_TITLES.has(key)
}

function splitLanguageSections(text: string): Array<[string, string]> | null {
  const lines = text.split(/\r?\n/)
  const headings: { index: number; title: string }[] = []
  lines.forEach((line, index) => {
    const match = LANG_HEADING_RE.exec(line.trim())
    if (match) headings.push({ index, title: match[2] })
  })
  if (!headings.length) return null
  return headings.map((heading, index) => {
    const start = heading.index + 1
    const end = index + 1 < headings.length ? headings[index + 1].index : lines.length
    return [heading.title, lines.slice(start, end).join('\n')]
  })
}

export function extractReleaseNotes(body: string | undefined, lang: Lang): string {
  const text = body?.trim() ?? ''
  if (!text) return ''
  const sections = splitLanguageSections(text)
  if (!sections) return text
  const match = sections.find(([title]) => isWantedLang(title, lang))
  if (match) return match[1].trim()
  return sections[0]?.[1]?.trim() || text
}

export function parseInline(text: string): InlineNode[] {
  const nodes: InlineNode[] = []
  let last = 0
  for (const match of text.matchAll(INLINE_RE)) {
    const index = match.index ?? 0
    if (index > last) nodes.push({ type: 'text', value: text.slice(last, index) })
    if (match[1] != null) nodes.push({ type: 'code', value: match[1] })
    else if (match[2] != null) nodes.push({ type: 'strong', value: match[2] })
    else if (match[3] != null) nodes.push({ type: 'em', value: match[3] })
    else if (match[4] != null && match[5] != null) nodes.push({ type: 'link', value: match[4], href: match[5] })
    last = index + match[0].length
  }
  if (last < text.length) nodes.push({ type: 'text', value: text.slice(last) })
  return nodes.length ? nodes : [{ type: 'text', value: text }]
}

function headingLevel(marks: string): 2 | 3 | 4 {
  if (marks.length <= 2) return 2
  if (marks.length === 3) return 3
  return 4
}

export function parseMarkdown(source: string): BlockNode[] {
  const lines = source.replace(/\r\n/g, '\n').split('\n')
  const blocks: BlockNode[] = []
  let index = 0
  while (index < lines.length) {
    const line = lines[index]
    if (!line.trim()) {
      index += 1
      continue
    }
    if (line.trim().startsWith('```')) {
      const code: string[] = []
      index += 1
      while (index < lines.length && !lines[index].trim().startsWith('```')) {
        code.push(lines[index])
        index += 1
      }
      if (index < lines.length) index += 1
      blocks.push({ type: 'code', text: code.join('\n') })
      continue
    }
    if (/^>\s?/.test(line.trim())) {
      const quote: string[] = []
      while (index < lines.length && /^>\s?/.test(lines[index].trim())) {
        quote.push(lines[index].trim().replace(/^>\s?/, ''))
        index += 1
      }
      blocks.push({ type: 'quote', text: quote.join(' ') })
      continue
    }
    const heading = /^(#{1,4})\s+(.+)$/.exec(line)
    if (heading) {
      blocks.push({ type: 'heading', level: headingLevel(heading[1]), text: heading[2].trim() })
      index += 1
      continue
    }
    const listed = LIST_RE.exec(line)
    if (listed) {
      const items: { text: string; depth: number }[] = []
      while (index < lines.length) {
        const item = LIST_RE.exec(lines[index])
        if (!item) break
        const depth = Math.min(2, Math.floor(item[1].replace(/\t/g, '  ').length / 2))
        let text = item[2]
        index += 1
        while (
          index < lines.length
          && /^\s{2,}\S/.test(lines[index])
          && !LIST_RE.test(lines[index])
          && !lines[index].trim().startsWith('```')
        ) {
          text += ` ${lines[index].trim()}`
          index += 1
        }
        items.push({ text, depth })
      }
      blocks.push({ type: 'list', items })
      continue
    }
    const paragraph = [line]
    index += 1
    while (
      index < lines.length
      && lines[index].trim()
      && !/^(#{1,4})\s+/.test(lines[index])
      && !LIST_RE.test(lines[index])
      && !lines[index].trim().startsWith('```')
      && !/^>\s?/.test(lines[index].trim())
    ) {
      paragraph.push(lines[index])
      index += 1
    }
    blocks.push({ type: 'paragraph', text: paragraph.join(' ').replace(/\s+/g, ' ').trim() })
  }
  return blocks
}
