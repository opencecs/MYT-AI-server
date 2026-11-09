/**
 * Isometric Person SVG Components
 *
 * Three minimal character figures viewed from slightly above and behind,
 * like looking at someone standing at a podium from the back of a classroom.
 *
 * Design principles:
 *   - Head: horizontal ellipse (wider than tall) = top-down view
 *   - No face = seen from behind
 *   - Small "ear" bumps on sides = reinforces behind-view
 *   - Isometric body: trapezoidal torso narrowing at waist
 *   - Subtle side offsets = 3/4 perspective
 *
 * Usage:
 *   <IsometricPerson variant="blue" />
 *   <IsometricPerson variant="orange" size={48} />
 *   <IsometricPerson variant="green" className="my-class" />
 */

import React from 'react';

/* ── Variant palettes ─────────────────────────────────────────── */

const PALETTES = {
  blue: {
    hair: '#3B4866',
    head: '#F5D0A9',
    neck: '#ECC89F',
    shirt: '#4B8BEE',
    shirtDark: '#3A6FCC',
    shirtDarker: '#2D5AAA',
    pants: '#3B4866',
    pantsDark: '#2E3850',
    shoes: '#2A2A2A',
    skinShadow: '#E8BF94',
    ear: '#EDCA9F',
  },
  orange: {
    hair: '#5A3E2B',
    head: '#F0C8A0',
    neck: '#E4B88E',
    shirt: '#F59E42',
    shirtDark: '#E08830',
    shirtDarker: '#C47020',
    pants: '#4A4A5A',
    pantsDark: '#38384A',
    shoes: '#2A2A2A',
    skinShadow: '#DDB07A',
    ear: '#E4B88E',
  },
  green: {
    hair: '#2D3A2D',
    head: '#EDCBA0',
    neck: '#DFBB8E',
    shirt: '#4CAF7D',
    shirtDark: '#3A9468',
    shirtDarker: '#2D7A54',
    pants: '#3A3A4A',
    pantsDark: '#2C2C3C',
    shoes: '#222222',
    skinShadow: '#D4AF7A',
    ear: '#DFBB8E',
  },
} as const;

type Variant = keyof typeof PALETTES;

/* ── Props ────────────────────────────────────────────────────── */

interface IsometricPersonProps {
  variant?: Variant;
  size?: number;
  className?: string;
}

/* ── Character 1: Standard standing person ────────────────────── */
/* Clean, symmetrical, most "icon-like" */

const StandardFigure: React.FC<{ c: typeof PALETTES.blue }> = ({ c }) => (
  <g>
    {/* Hair (top of head, wide ellipse = top-down view) */}
    <ellipse cx="20" cy="10" rx="10" ry="6.5" fill={c.hair} />
    {/* Head */}
    <ellipse cx="20" cy="11" rx="9" ry="5.8" fill={c.head} />
    {/* Ears (visible from behind) */}
    <ellipse cx="11" cy="12" rx="1.8" ry="2.8" fill={c.ear} />
    <ellipse cx="29" cy="12" rx="1.8" ry="2.8" fill={c.ear} />
    {/* Neck */}
    <rect x="17" y="14" width="6" height="4" rx="1.5" fill={c.neck} />
    {/* Torso (slightly trapezoidal = isometric shoulders) */}
    <path
      d={`M11 18 L29 18 L27 32 L13 32 Z`}
      fill={c.shirt}
    />
    {/* Shirt shadow side (right = darker, isometric lighting) */}
    <path
      d={`M22 18 L29 18 L27 32 L22 32 Z`}
      fill={c.shirtDark}
    />
    {/* Collar detail */}
    <path d="M17 18 L20 22 L23 18" fill="none" stroke={c.shirtDarker} strokeWidth="0.8" />
    {/* Left arm */}
    <path
      d={`M11 18 L9 19 L8 30 L11 31 L13 19 Z`}
      fill={c.shirt}
    />
    {/* Right arm (darker) */}
    <path
      d={`M29 18 L31 19 L32 30 L29 31 L27 19 Z`}
      fill={c.shirtDark}
    />
    {/* Pants (slight isometric offset) */}
    <path d="M13 32 L20 32 L20 44 L14 44 Z" fill={c.pants} />
    <path d="M20 32 L27 32 L26 44 L20 44 Z" fill={c.pantsDark} />
    {/* Shoes */}
    <rect x="12.5" y="44" width="7" height="3" rx="1.5" fill={c.shoes} />
    <rect x="19.5" y="44" width="7" height="3" rx="1.5" fill={c.shoes} />
  </g>
);

/* ── Character 2: Slightly turned (3/4 back view) ─────────────── */
/* Head tilted slightly right, one arm slightly forward = more dynamic */

const TurnedFigure: React.FC<{ c: typeof PALETTES.blue }> = ({ c }) => (
  <g>
    {/* Hair - slightly offset for 3/4 turn */}
    <ellipse cx="21" cy="10" rx="10" ry="6" fill={c.hair} />
    {/* Head */}
    <ellipse cx="21" cy="11" rx="9" ry="5.5" fill={c.head} />
    {/* Left ear (slightly visible) */}
    <ellipse cx="12" cy="12" rx="1.5" ry="2.5" fill={c.ear} />
    {/* Right ear (more visible due to turn) */}
    <ellipse cx="30" cy="12" rx="2" ry="2.8" fill={c.ear} />
    {/* Neck (offset slightly) */}
    <rect x="18" y="14" width="6" height="4" rx="1.5" fill={c.neck} />
    {/* Torso - asymmetrical for 3/4 view */}
    <path
      d={`M12 18 L30 18 L27 33 L14 33 Z`}
      fill={c.shirt}
    />
    <path
      d={`M23 18 L30 18 L27 33 L23 33 Z`}
      fill={c.shirtDark}
    />
    {/* Collar */}
    <path d="M18 18 L21 21.5 L24 18" fill="none" stroke={c.shirtDarker} strokeWidth="0.7" />
    {/* Left arm (slightly back) */}
    <path
      d={`M12 18 L10 19 L9 30 L12 32 L14 20 Z`}
      fill={c.shirt}
    />
    {/* Right arm (slightly forward) */}
    <path
      d={`M30 18 L33 20 L34 30 L30 31 L28 20 Z`}
      fill={c.shirtDark}
    />
    {/* Hand (right arm forward, hint of hand) */}
    <ellipse cx="33" cy="30" rx="2" ry="1.5" fill={c.skinShadow} />
    {/* Pants */}
    <path d="M14 33 L21 33 L21 44 L15 44 Z" fill={c.pants} />
    <path d="M21 33 L27 33 L26 44 L21 44 Z" fill={c.pantsDark} />
    {/* Shoes */}
    <rect x="13.5" y="44" width="7" height="3" rx="1.5" fill={c.shoes} />
    <rect x="20" y="44" width="7" height="3" rx="1.5" fill={c.shoes} />
  </g>
);

/* ── Character 3: Short/compact figure ─────────────────────────── */
/* Broader shoulders, slightly stockier - good for variety */

const CompactFigure: React.FC<{ c: typeof PALETTES.blue }> = ({ c }) => (
  <g>
    {/* Hair - broader head */}
    <ellipse cx="20" cy="10" rx="11" ry="6" fill={c.hair} />
    {/* Head */}
    <ellipse cx="20" cy="11" rx="10" ry="5.5" fill={c.head} />
    {/* Ears */}
    <ellipse cx="10" cy="12.5" rx="2" ry="2.5" fill={c.ear} />
    <ellipse cx="30" cy="12.5" rx="2" ry="2.5" fill={c.ear} />
    {/* Neck (thicker) */}
    <rect x="16" y="14" width="8" height="4" rx="2" fill={c.neck} />
    {/* Torso (broader shoulders) */}
    <path
      d={`M8 18 L32 18 L29 31 L11 31 Z`}
      fill={c.shirt}
    />
    <path
      d={`M23 18 L32 18 L29 31 L23 31 Z`}
      fill={c.shirtDark}
    />
    {/* Collar (round neck) */}
    <path d="M16 18 Q20 22 24 18" fill="none" stroke={c.shirtDarker} strokeWidth="0.8" />
    {/* Left arm */}
    <path
      d={`M8 18 L6 20 L5 29 L8 30 L11 20 Z`}
      fill={c.shirt}
    />
    {/* Right arm */}
    <path
      d={`M32 18 L34 20 L35 29 L32 30 L29 20 Z`}
      fill={c.shirtDark}
    />
    {/* Pants (wider stance) */}
    <path d="M11 31 L20 31 L19 44 L12 44 Z" fill={c.pants} />
    <path d="M20 31 L29 31 L28 44 L21 44 Z" fill={c.pantsDark} />
    {/* Shoes (slightly bigger) */}
    <rect x="10.5" y="44" width="8" height="3.5" rx="2" fill={c.shoes} />
    <rect x="20.5" y="44" width="8" height="3.5" rx="2" fill={c.shoes} />
  </g>
);

/* ── Main component ───────────────────────────────────────────── */

const FIGURES = {
  standard: StandardFigure,
  turned: TurnedFigure,
  compact: CompactFigure,
} as const;

type FigureStyle = keyof typeof FIGURES;

interface IsometricPersonFullProps extends IsometricPersonProps {
  /** Which figure style. Default: "standard" */
  style?: FigureStyle;
}

export const IsometricPerson: React.FC<IsometricPersonFullProps> = ({
  variant = 'blue',
  size = 40,
  className,
  style: figureStyle = 'standard',
}) => {
  const colors = PALETTES[variant] as typeof PALETTES.blue;
  const Figure = FIGURES[figureStyle];

  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 40 48"
      width={size}
      height={size * 1.2}
      className={className}
      aria-hidden="true"
    >
      <Figure c={colors} />
    </svg>
  );
};

/* ── Convenience: all three variants at once (for previews) ───── */

export const IsometricPersonGallery: React.FC<{
  size?: number;
  className?: string;
}> = ({ size = 40, className }) => (
  <div className={`flex items-end gap-2 ${className ?? ''}`}>
    <IsometricPerson variant="blue" style="standard" size={size} />
    <IsometricPerson variant="orange" style="turned" size={size} />
    <IsometricPerson variant="green" style="compact" size={size} />
  </div>
);

export default IsometricPerson;
