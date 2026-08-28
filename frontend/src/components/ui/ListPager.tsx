import { Button, ButtonGroup } from '@heroui/react'
import { CaretLeft, CaretRight } from '@phosphor-icons/react'

export const PAGE_SIZES = [20, 50, 100] as const
export type PageSize = (typeof PAGE_SIZES)[number]

export function ListPager({
  total,
  page,
  pageCount,
  pageSize,
  loading,
  pageSizeLabel,
  pageLabel,
  prevLabel,
  nextLabel,
  onPage,
  onPageSize,
}: {
  total: number
  page: number
  pageCount: number
  pageSize: PageSize
  loading?: boolean
  pageSizeLabel: string
  pageLabel: string
  prevLabel: string
  nextLabel: string
  onPage: (page: number) => void
  onPageSize: (size: PageSize) => void
}) {
  if (total <= 0) return null
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex items-center gap-2">
        <span className="text-xs text-[var(--app-faint)]">{pageSizeLabel}</span>
        <ButtonGroup className="toolbar-group">
          {PAGE_SIZES.map((size) => (
            <Button
              key={size}
              size="sm"
              variant={pageSize === size ? 'secondary' : 'ghost'}
              onPress={() => onPageSize(size)}
            >
              {size}
            </Button>
          ))}
        </ButtonGroup>
      </div>
      <div className="flex items-center gap-2">
        <span className="mono text-[11px] text-[var(--app-faint)]">{pageLabel}</span>
        <Button
          isIconOnly
          size="sm"
          variant="ghost"
          isDisabled={loading || page <= 1}
          onPress={() => onPage(Math.max(1, page - 1))}
          aria-label={prevLabel}
        >
          <CaretLeft size={14} />
        </Button>
        <Button
          isIconOnly
          size="sm"
          variant="ghost"
          isDisabled={loading || page >= pageCount}
          onPress={() => onPage(Math.min(pageCount, page + 1))}
          aria-label={nextLabel}
        >
          <CaretRight size={14} />
        </Button>
      </div>
    </div>
  )
}
