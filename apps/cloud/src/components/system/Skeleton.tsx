import type { CSSProperties } from 'react'

interface SkeletonProps {
  width?: number | string
  height?: number | string
  radius?: number | string
  className?: string
  style?: CSSProperties
}

/** Neutral loading block. `aria-hidden`; the region announces loading once. */
export function Skeleton({ width = '100%', height = 16, radius, className, style }: SkeletonProps) {
  return (
    <span
      aria-hidden="true"
      className={`skeleton${className ? ` ${className}` : ''}`}
      style={{ display: 'block', width, height, borderRadius: radius, ...style }}
    />
  )
}

export function SkeletonLines({ lines = 3 }: { lines?: number }) {
  return (
    <div className="skeleton-lines" aria-hidden="true">
      {Array.from({ length: lines }, (_, index) => (
        <Skeleton key={index} height={14} width={index === lines - 1 ? '60%' : '100%'} />
      ))}
    </div>
  )
}

export function SkeletonCards({ count = 3 }: { count?: number }) {
  return (
    <div className="grid grid--cards" aria-hidden="true">
      {Array.from({ length: count }, (_, index) => (
        <div className="card" key={index}>
          <Skeleton height={18} width="55%" />
          <div style={{ height: 8 }} />
          <SkeletonLines lines={2} />
        </div>
      ))}
    </div>
  )
}

export function SkeletonRows({ count = 4 }: { count?: number }) {
  return (
    <div className="data-list" aria-hidden="true">
      {Array.from({ length: count }, (_, index) => (
        <div className="data-list__row" key={index}>
          <div className="data-list__main">
            <Skeleton height={16} width="40%" />
          </div>
          <Skeleton height={16} width={80} />
        </div>
      ))}
    </div>
  )
}
