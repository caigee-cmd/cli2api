import { useRef, type KeyboardEvent } from 'react'
import { Button, TextArea } from '@heroui/react'
import { Paperclip, PaperPlaneTilt, Stop, X } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import type { ChatAttachment } from './types'
import { IMAGE_ATTACHMENT_MIMES, TEXT_ATTACHMENT_EXTS, attachmentExt } from './types'

function formatSize(size: number) {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / (1024 * 1024)).toFixed(1)} MB`
}

async function readAttachment(file: File): Promise<ChatAttachment | null> {
  const ext = attachmentExt(file.name)
  const mime = file.type || 'application/octet-stream'
  const id = `${file.name}-${file.size}-${file.lastModified}-${Math.random().toString(36).slice(2, 8)}`
  if (IMAGE_ATTACHMENT_MIMES.has(mime) || ['png', 'jpg', 'jpeg', 'gif', 'webp'].includes(ext)) {
    const dataUrl = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(String(reader.result || ''))
      reader.onerror = () => reject(reader.error || new Error('read failed'))
      reader.readAsDataURL(file)
    })
    return { id, name: file.name, mime, kind: 'image', dataUrl, size: file.size }
  }
  if (TEXT_ATTACHMENT_EXTS.has(ext) || mime.startsWith('text/') || mime === 'application/json') {
    const text = await file.text()
    return { id, name: file.name, mime, kind: 'text', text, size: file.size }
  }
  return null
}

export function ChatComposer({
  value,
  onChange,
  attachments,
  onAttachmentsChange,
  disabled,
  streaming,
  canSend,
  onSend,
  onStop,
}: {
  value: string
  onChange: (value: string) => void
  attachments: ChatAttachment[]
  onAttachmentsChange: (next: ChatAttachment[]) => void
  disabled?: boolean
  streaming: boolean
  canSend: boolean
  onSend: () => void
  onStop: () => void
}) {
  const { t } = useI18n()
  const fileRef = useRef<HTMLInputElement>(null)

  async function onPickFiles(files: FileList | null) {
    if (!files?.length) return
    const accepted: ChatAttachment[] = []
    const rejected: string[] = []
    for (const file of Array.from(files)) {
      try {
        const item = await readAttachment(file)
        if (item) accepted.push(item)
        else rejected.push(file.name)
      } catch {
        rejected.push(file.name)
      }
    }
    if (accepted.length) onAttachmentsChange([...attachments, ...accepted])
    if (rejected.length) {
      window.alert(t('chatAttachUnsupported', { names: rejected.join(', ') }))
    }
    if (fileRef.current) fileRef.current.value = ''
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return
    event.preventDefault()
    if (!streaming && canSend) onSend()
  }

  return (
    <div className="space-y-3 border-t border-separator p-5 sm:p-6">
      {attachments.length ? (
        <div className="flex flex-wrap gap-2">
          {attachments.map((file) => (
            <span
              key={file.id}
              className="inline-flex max-w-full items-center gap-1.5 rounded-md bg-surface-secondary px-2 py-1 text-caption text-foreground"
            >
              <span className="truncate">{file.name}</span>
              <span className="mono text-muted">{formatSize(file.size)}</span>
              <button
                type="button"
                className="grid size-4 place-items-center text-muted hover:text-foreground"
                aria-label={t('chatAttachRemove')}
                onClick={() => onAttachmentsChange(attachments.filter((item) => item.id !== file.id))}
              >
                <X size={11} />
              </button>
            </span>
          ))}
        </div>
      ) : null}

      <TextArea
        fullWidth
        rows={4}
        className="min-h-28 resize-y px-4 py-3 text-body leading-6"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={onKeyDown}
        placeholder={t('promptPlaceholder')}
        aria-label={t('prompt')}
        disabled={disabled || streaming}
      />
      <p className="text-caption leading-5 text-muted">{t('chatTryHelper')}</p>
      <div className="flex items-center gap-2">
        <input
          ref={fileRef}
          type="file"
          className="hidden"
          multiple
          accept=".txt,.md,.json,.csv,.yml,.yaml,.toml,.xml,.html,.css,.js,.jsx,.ts,.tsx,.py,.go,.rs,.sh,.sql,.png,.jpg,.jpeg,.gif,.webp,text/*,image/png,image/jpeg,image/gif,image/webp,application/json"
          onChange={(event) => void onPickFiles(event.target.files)}
        />
        <Button
          isIconOnly
          variant="secondary"
          aria-label={t('chatAttach')}
          isDisabled={disabled || streaming}
          onPress={() => fileRef.current?.click()}
        >
          <Paperclip size={16} />
        </Button>
        {streaming ? (
          <Button fullWidth variant="secondary" onPress={onStop}>
            <Stop size={16} />
            {t('chatTryStop')}
          </Button>
        ) : (
          <Button fullWidth isDisabled={!canSend || disabled} onPress={onSend}>
            <PaperPlaneTilt size={16} />
            {t('chatTrySend')}
          </Button>
        )}
      </div>
    </div>
  )
}
