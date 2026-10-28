import { useRef, type KeyboardEvent, type ReactNode } from 'react'
import { Button, Label, ListBox, Select, TextArea } from '@heroui/react'
import { FileText, PaperPlaneTilt, Plus, Stop, X } from '@phosphor-icons/react'
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

export type ComposerOption = {
  id: string
  textValue: string
  label: string
  hint?: string
  icon?: ReactNode
}

export function CompactSelect({
  ariaLabel,
  value,
  onChange,
  placeholder,
  isDisabled,
  options,
  className,
  tone = 'default',
}: {
  ariaLabel: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  isDisabled?: boolean
  options: ComposerOption[]
  className?: string
  tone?: 'default' | 'muted'
}) {
  const selected = options.find((option) => option.id === value)
  const triggerTone =
    tone === 'muted'
      ? 'text-muted hover:text-foreground'
      : 'text-foreground'
  return (
    <Select
      className={className ?? 'w-auto max-w-[11rem] min-w-0'}
      aria-label={ariaLabel}
      value={value || null}
      placeholder={placeholder}
      isDisabled={isDisabled}
      onChange={(next) => {
        if (typeof next === 'string' && next) onChange(next)
      }}
    >
      <Select.Trigger
        className={`h-8 min-h-8 gap-1 rounded-[10px] border-0 bg-transparent px-2.5 shadow-none items-center hover:bg-surface-secondary/80 data-[focus-visible=true]:bg-surface-secondary/80 ${triggerTone}`}
      >
        <Select.Value className="min-w-0 truncate text-caption font-medium">
          {({ defaultChildren, isPlaceholder }) => {
            if (isPlaceholder || !selected) return defaultChildren
            return (
              <span className="flex min-w-0 items-center gap-1">
                {selected.icon ? <span className="grid size-3.5 shrink-0 place-items-center opacity-80">{selected.icon}</span> : null}
                <span className="truncate">{selected.label}</span>
              </span>
            )
          }}
        </Select.Value>
        <Select.Indicator className="size-2.5 opacity-50" />
      </Select.Trigger>
      <Select.Popover placement="top end" className="max-h-72 min-w-48">
        <ListBox>
          {options.map((option) => (
            <ListBox.Item key={option.id} id={option.id} textValue={option.textValue} className="gap-2">
              {option.icon ? <span className="grid size-5 shrink-0 place-items-center">{option.icon}</span> : null}
              <div className="min-w-0 flex-1">
                <Label className="block truncate text-caption">{option.label}</Label>
                {option.hint ? <span className="block truncate text-caption text-muted">{option.hint}</span> : null}
              </div>
              <ListBox.ItemIndicator />
            </ListBox.Item>
          ))}
        </ListBox>
      </Select.Popover>
    </Select>
  )
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
  accountValue,
  onAccountChange,
  accountOptions,
  modelValue,
  onModelChange,
  modelOptions,
  modelsLoading,
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
  accountValue: string
  onAccountChange: (value: string) => void
  accountOptions: ComposerOption[]
  modelValue: string
  onModelChange: (value: string) => void
  modelOptions: ComposerOption[]
  modelsLoading?: boolean
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
    <div className="access-composer px-3 pb-3 pt-2.5 sm:px-4 sm:pb-3.5 sm:pt-3">
      <div className="access-composer-well overflow-hidden rounded-2xl">
        {attachments.length ? (
          <div className="flex flex-wrap gap-2 px-3 pt-3">
            {attachments.map((file) => (
              <span
                key={file.id}
                className="inline-flex max-w-[14rem] items-center gap-2 rounded-xl bg-surface/80 px-1.5 py-1.5 text-caption text-foreground"
              >
                {file.kind === 'image' && file.dataUrl ? (
                  <img src={file.dataUrl} alt="" className="size-8 shrink-0 rounded-lg object-cover" />
                ) : (
                  <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-secondary text-muted">
                    <FileText size={14} />
                  </span>
                )}
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium">{file.name}</span>
                  <span className="mono text-muted">{formatSize(file.size)}</span>
                </span>
                <button
                  type="button"
                  className="grid size-6 shrink-0 place-items-center rounded-md text-muted hover:bg-surface-secondary hover:text-foreground"
                  aria-label={t('chatAttachRemove')}
                  onClick={() => onAttachmentsChange(attachments.filter((item) => item.id !== file.id))}
                >
                  <X size={12} />
                </button>
              </span>
            ))}
          </div>
        ) : null}

        <TextArea
          fullWidth
          rows={3}
          className="min-h-20 resize-none border-0 bg-transparent px-3.5 py-3 text-body leading-[1.65] shadow-none focus-visible:ring-0"
          value={value}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={onKeyDown}
          placeholder={t('promptPlaceholder')}
          aria-label={t('prompt')}
          disabled={disabled || streaming}
        />

        <div className="flex items-center gap-0.5 px-2 pb-2 pt-0.5">
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
            size="sm"
            variant="ghost"
            className="size-8 min-w-8 shrink-0 rounded-[10px]"
            aria-label={t('chatAttach')}
            isDisabled={disabled || streaming}
            onPress={() => fileRef.current?.click()}
          >
            <Plus size={15} />
          </Button>

          <div className="min-w-0 flex-1" />

          <CompactSelect
            ariaLabel={t('account')}
            value={accountValue}
            onChange={onAccountChange}
            options={accountOptions}
            isDisabled={disabled || streaming}
            tone="muted"
            className="w-auto max-w-[8.5rem] min-w-0"
          />
          <CompactSelect
            ariaLabel={t('model')}
            value={modelValue}
            onChange={onModelChange}
            placeholder={modelsLoading ? t('requesting') : t('model')}
            options={modelOptions}
            isDisabled={disabled || streaming || modelsLoading || !modelOptions.length}
            className="w-auto max-w-[10.5rem] min-w-0"
          />

          {/* Fixed slot: send ↔ stop swap without shoving the composer row. */}
          <div className="relative size-8 shrink-0">
            <Button
              isIconOnly
              size="sm"
              variant="secondary"
              className={`absolute inset-0 size-8 rounded-[10px] transition-opacity duration-200 ease-[cubic-bezier(0.2,0.7,0.2,1)] ${streaming ? 'opacity-100' : 'pointer-events-none opacity-0'}`}
              aria-label={t('chatTryStop')}
              onPress={onStop}
            >
              <Stop size={14} weight="fill" />
            </Button>
            <Button
              isIconOnly
              size="sm"
              className={`absolute inset-0 size-8 rounded-[10px] transition-opacity duration-200 ease-[cubic-bezier(0.2,0.7,0.2,1)] ${streaming ? 'pointer-events-none opacity-0' : 'opacity-100'}`}
              aria-label={t('chatTrySend')}
              isDisabled={!canSend || disabled || streaming}
              onPress={onSend}
            >
              <PaperPlaneTilt size={14} weight="fill" />
            </Button>
          </div>
        </div>
      </div>
      <p className="mt-1 px-1 text-caption leading-4 text-muted/65">{t('chatTryHelper')}</p>
    </div>
  )
}
