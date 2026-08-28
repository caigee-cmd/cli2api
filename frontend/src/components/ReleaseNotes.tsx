import { useMemo } from 'react'
import { parseInline, parseMarkdown, type InlineNode } from '@/lib/releaseNotes'

function InlineText({ text }: { text: string }) {
  const nodes = useMemo(() => parseInline(text), [text])
  return (
    <>
      {nodes.map((node, index) => (
        <InlineNodeView key={`${node.type}-${index}`} node={node} />
      ))}
    </>
  )
}

function InlineNodeView({ node }: { node: InlineNode }) {
  if (node.type === 'code') return <code>{node.value}</code>
  if (node.type === 'strong') return <strong>{node.value}</strong>
  if (node.type === 'link') {
    return <a href={node.href} target="_blank" rel="noreferrer">{node.value}</a>
  }
  return node.value
}

export function ReleaseNotes({ markdown, emptyLabel }: { markdown: string; emptyLabel: string }) {
  const blocks = useMemo(() => parseMarkdown(markdown), [markdown])
  if (!markdown.trim() || !blocks.length) {
    return <p className="release-notes__empty">{emptyLabel}</p>
  }
  return (
    <div className="release-notes">
      {blocks.map((block, index) => {
        if (block.type === 'heading') {
          const Tag = block.level === 2 ? 'h3' : 'h4'
          return <Tag key={`h-${index}`}><InlineText text={block.text} /></Tag>
        }
        if (block.type === 'list') {
          return (
            <ul key={`l-${index}`}>
              {block.items.map((item, itemIndex) => (
                <li key={`i-${index}-${itemIndex}`}><InlineText text={item} /></li>
              ))}
            </ul>
          )
        }
        return <p key={`p-${index}`}><InlineText text={block.text} /></p>
      })}
    </div>
  )
}
