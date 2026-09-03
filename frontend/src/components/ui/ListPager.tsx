import { Pagination, ToggleButton, ToggleButtonGroup } from '@heroui/react'

export const PAGE_SIZES = [20, 50, 100] as const
export const ACCOUNT_PAGE_SIZES = [5, 20, 50, 100] as const
export type PageSize = (typeof ACCOUNT_PAGE_SIZES)[number]

function pageTokens(page: number, pageCount: number): Array<number | 'gap'> {
  if (pageCount <= 7) return Array.from({ length: pageCount }, (_, index) => index + 1)
  const tokens: Array<number | 'gap'> = [1]
  if (page > 3) tokens.push('gap')
  const lo = Math.max(2, page - 1)
  const hi = Math.min(pageCount - 1, page + 1)
  for (let n = lo; n <= hi; n++) tokens.push(n)
  if (page < pageCount - 2) tokens.push('gap')
  tokens.push(pageCount)
  return tokens
}

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
  pageSizes = PAGE_SIZES,
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
  pageSizes?: readonly PageSize[]
}) {
  if (total <= 0) return null
  const tokens = pageTokens(page, pageCount)

  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex items-center gap-2">
        <span className="text-xs text-muted">{pageSizeLabel}</span>
        <ToggleButtonGroup
          size="sm"
          selectionMode="single"
          disallowEmptySelection
          selectedKeys={new Set([String(pageSize)])}
          onSelectionChange={(keys) => {
            const next = Number([...keys][0])
            if (pageSizes.includes(next as PageSize)) onPageSize(next as PageSize)
          }}
          aria-label={pageSizeLabel}
        >
          {pageSizes.map((size) => (
            <ToggleButton key={size} id={String(size)}>{size}</ToggleButton>
          ))}
        </ToggleButtonGroup>
      </div>
      <Pagination size="sm" className="w-fit">
        <Pagination.Summary className="mono text-[11px]">{pageLabel}</Pagination.Summary>
        <Pagination.Content>
          <Pagination.Item>
            <Pagination.Previous
              isDisabled={loading || page <= 1}
              onPress={() => onPage(Math.max(1, page - 1))}
              aria-label={prevLabel}
            >
              <Pagination.PreviousIcon />
            </Pagination.Previous>
          </Pagination.Item>
          {tokens.map((token, index) => (
            token === 'gap' ? (
              <Pagination.Item key={`gap-${index}`}>
                <Pagination.Ellipsis />
              </Pagination.Item>
            ) : (
              <Pagination.Item key={token}>
                <Pagination.Link
                  isActive={token === page}
                  isDisabled={loading}
                  onPress={() => onPage(token)}
                >
                  {token}
                </Pagination.Link>
              </Pagination.Item>
            )
          ))}
          <Pagination.Item>
            <Pagination.Next
              isDisabled={loading || page >= pageCount}
              onPress={() => onPage(Math.min(pageCount, page + 1))}
              aria-label={nextLabel}
            >
              <Pagination.NextIcon />
            </Pagination.Next>
          </Pagination.Item>
        </Pagination.Content>
      </Pagination>
    </div>
  )
}
