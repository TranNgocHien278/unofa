import type { Card as CardT, Color } from '../api/types'
import { i18n } from '../i18n'

// Each colour carries its own shape, so a red-green colour-blind player reads the
// card by shape alone (06 §Component lá bài).
export const COLORS: Exclude<Color, 'NONE'>[] = ['RED', 'YELLOW', 'GREEN', 'BLUE']

export const colorVar = (c: Color) => (c === 'NONE' ? 'var(--card-ink)' : `var(--card-${c.toLowerCase()})`)

export function cardLabel(c: CardT): string {
  const rank = i18n.t(`card.rank.${c.rank}`)
  return c.color === 'NONE' ? rank : i18n.t('card.label', { color: i18n.t(`card.color.${c.color}`), rank })
}

const INK = 'var(--card-ink)'
const PAPER = 'var(--card-paper)'

export function Shape({ color, size = 1, x = 0, y = 0, fill, stroke = INK, sw = 4 }: {
  color: Exclude<Color, 'NONE'>
  size?: number
  x?: number
  y?: number
  fill?: string
  stroke?: string
  sw?: number
}) {
  const f = fill ?? colorVar(color)
  const t = `translate(${x} ${y}) scale(${size})`
  const common = { fill: f, stroke, strokeWidth: sw / size, strokeLinejoin: 'round' as const, transform: t }
  switch (color) {
    case 'RED':
      return <circle cx={0} cy={0} r={30} {...common} />
    case 'YELLOW':
      return <path d="M0 -33 L33 25 L-33 25 Z" {...common} />
    case 'GREEN':
      return <rect x={-27} y={-27} width={54} height={54} rx={7} {...common} />
    case 'BLUE':
      return <path d="M0 -35 L35 0 L0 35 L-35 0 Z" {...common} />
  }
}

function Glyph({ rank, fill, outline }: { rank: CardT['rank']; fill: string; outline?: boolean }) {
  if (rank === 'SKIP' || rank === 'REVERSE') {
    const d =
      rank === 'SKIP'
        ? 'M64 75 a14 14 0 1 1 -28 0 a14 14 0 1 1 28 0 M40 85 L60 65'
        : 'M38 70 Q38 60 50 60 L62 60 M56 54 L62 60 L56 66 M62 80 Q62 90 50 90 L38 90 M44 84 L38 90 L44 96'
    return (
      <g fill="none" strokeLinecap="round" strokeLinejoin="round">
        {outline && <path d={d} stroke={INK} strokeWidth={14} />}
        <path d={d} stroke={fill} strokeWidth={6} />
      </g>
    )
  }
  const text = rank === 'DRAW2' ? '+2' : rank
  return (
    <text x={50} y={rank === 'DRAW2' ? 87 : 89} textAnchor="middle" fontFamily="var(--font-display)" fontSize={rank === 'DRAW2' ? 30 : 38} fill={fill}>
      {text}
    </text>
  )
}

// Corner index: text for numbers and +2/W/+4, the drawn glyph for skip/reverse.
function Corner({ rank, rotate }: { rank: CardT['rank']; rotate?: boolean }) {
  const t = rotate ? 'rotate(180 50 75)' : undefined
  if (rank === 'SKIP' || rank === 'REVERSE')
    return (
      <g transform={t}>
        <g transform="translate(20 20) scale(0.42) translate(-50 -75)">
          <Glyph rank={rank} fill={PAPER} outline />
        </g>
      </g>
    )
  const label = rank === 'DRAW2' ? '+2' : rank === 'WILD' ? 'W' : rank === 'WILD4' ? '+4' : rank
  return (
    <text x={13} y={27} transform={t} fontFamily="var(--font-display)" fontSize={20} fill={PAPER} stroke={INK} strokeWidth={4} paintOrder="stroke">
      {label}
    </text>
  )
}

// Wild faces: the four shapes around the centre on an ink body.
function WildFace({ four }: { four: boolean }) {
  // +4 gets its own row under a tighter grid, never on top of the shapes.
  const [size, l, r, t, b] = four ? [0.32, 39, 61, 50, 72] : [0.42, 36, 64, 61, 90]
  return (
    <g>
      <Shape color="RED" size={size} x={l} y={t} stroke={PAPER} sw={3} />
      <Shape color="YELLOW" size={size} x={r} y={t} stroke={PAPER} sw={3} />
      <Shape color="GREEN" size={size} x={l} y={b} stroke={PAPER} sw={3} />
      <Shape color="BLUE" size={size} x={r} y={b} stroke={PAPER} sw={3} />
      {four && (
        <text x={50} y={116} textAnchor="middle" fontFamily="var(--font-display)" fontSize={32} fill={PAPER} stroke={INK} strokeWidth={5} paintOrder="stroke">
          +4
        </text>
      )}
    </g>
  )
}

const centerY: Record<Exclude<Color, 'NONE'>, number> = { RED: 75, YELLOW: 80, GREEN: 75, BLUE: 75 }

export function CardFace({ card }: { card: CardT }) {
  const wild = card.color === 'NONE'
  const body = colorVar(card.color)
  return (
    <svg viewBox="0 0 100 150" className="card-svg" aria-hidden="true" focusable="false">
      <rect x={2.5} y={2.5} width={95} height={145} rx={11} fill={body} stroke={INK} strokeWidth={5} />
      {wild ? (
        <WildFace four={card.rank === 'WILD4'} />
      ) : (
        <>
          <Shape color={card.color as Exclude<Color, 'NONE'>} x={50} y={centerY[card.color as Exclude<Color, 'NONE'>]} fill={PAPER} />
          <g transform={card.color === 'YELLOW' ? 'translate(0 6)' : undefined}>
            <Glyph rank={card.rank} fill={INK} />
          </g>
        </>
      )}
      <Corner rank={card.rank} />
      <Corner rank={card.rank} rotate />
      {!wild && <Shape color={card.color as Exclude<Color, 'NONE'>} size={0.2} x={19} y={38} fill={PAPER} sw={2.5} />}
    </svg>
  )
}

export function CardBack() {
  return (
    <svg viewBox="0 0 100 150" className="card-svg" aria-hidden="true" focusable="false">
      <rect x={2.5} y={2.5} width={95} height={145} rx={11} fill={INK} stroke={INK} strokeWidth={5} />
      <rect x={11} y={11} width={78} height={128} rx={6} fill="none" stroke="var(--sun)" strokeWidth={2.5} strokeDasharray="1 6" strokeLinecap="round" />
      <g transform="rotate(-14 50 75)">
        <rect x={22} y={46} width={56} height={58} rx={12} fill="var(--sun)" stroke={PAPER} strokeWidth={3} />
        <path d="M37 60 v14 a13 13 0 0 0 26 0 V60" fill="none" stroke={INK} strokeWidth={8} strokeLinecap="round" />
      </g>
    </svg>
  )
}
