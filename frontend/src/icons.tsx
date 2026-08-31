import React from 'react'

type P = { size?: number; className?: string }
const stroke = (size = 18, className?: string): React.SVGProps<SVGSVGElement> => ({
  width: size, height: size, viewBox: '0 0 24 24', fill: 'none',
  stroke: 'currentColor', strokeWidth: 1.6, strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const, className, 'aria-hidden': true,
})

// Raven mark: Phosphor "Bird" (MIT). See ATTRIBUTION.md.
export const RavenMark = ({ size = 24, className }: P) => (
  <svg width={size} height={size} viewBox="0 0 256 256" fill="currentColor" className={className} aria-hidden>
    <path d="M236.44,73.34,213.21,57.86A60,60,0,0,0,156,16h-.29C122.79,16.16,96,43.47,96,76.89V96.63L11.63,197.88l-.1.12A16,16,0,0,0,24,224h88A104.11,104.11,0,0,0,216,120V100.28l20.44-13.62a8,8,0,0,0,0-13.32ZM126.15,133.12l-60,72a8,8,0,1,1-12.29-10.24l60-72a8,8,0,1,1,12.29,10.24ZM164,80a12,12,0,1,1,12-12A12,12,0,0,1,164,80Z" />
  </svg>
)

export const Gear = ({ size, className }: P) => (
  <svg {...stroke(size, className)}>
    <path d="M4 7h9M19 7h1M4 17h1M11 17h9" />
    <circle cx="16" cy="7" r="2.2" /><circle cx="8" cy="17" r="2.2" />
  </svg>
)
export const Monitor = ({ size, className }: P) => (
  <svg {...stroke(size, className)}><rect x="3" y="4.5" width="18" height="12" rx="1.5" /><path d="M2 20.5h20" /></svg>
)
export const Phone = ({ size, className }: P) => (
  <svg {...stroke(size, className)}><rect x="6" y="3" width="12" height="18" rx="2.5" /><path d="M11 18h2" /></svg>
)
export const FileGlyph = ({ size, className }: P) => (
  <svg {...stroke(size, className)}><path d="M14 3v5h5M14 3H6v18h12V8z" /></svg>
)
export const FolderGlyph = ({ size, className }: P) => (
  <svg {...stroke(size, className)}><path d="M3 6.5A1.5 1.5 0 0 1 4.5 5h4l2 2.5h7A1.5 1.5 0 0 1 19 9v8.5A1.5 1.5 0 0 1 17.5 19h-13A1.5 1.5 0 0 1 3 17.5z" /></svg>
)
export const ArrowDown = ({ size, className }: P) => (
  <svg {...stroke(size, className)} strokeWidth={1.8}><path d="M12 5v14M6 13l6 6 6-6" /></svg>
)
export const ArrowUp = ({ size, className }: P) => (
  <svg {...stroke(size, className)} strokeWidth={1.8}><path d="M12 19V5M6 11l6-6 6 6" /></svg>
)
export const Wifi = ({ size, className }: P) => (
  <svg {...stroke(size, className)}><path d="M5 12.5a10 10 0 0 1 14 0M8.5 16a5 5 0 0 1 7 0M2 9a15 15 0 0 1 20 0" /><circle cx="12" cy="19.5" r="1.1" fill="currentColor" stroke="none" /></svg>
)
export const WifiOff = ({ size, className }: P) => (
  <svg {...stroke(size, className)}><path d="M2 9a15 15 0 0 1 20 0M5 12.5a10 10 0 0 1 14 0M8.5 16a5 5 0 0 1 7 0" /><line x1="3" y1="3" x2="21" y2="21" /></svg>
)
export const Shield = ({ size, className }: P) => (
  <svg {...stroke(size, className)}><path d="M12 3l7 3v6c0 4-3 7-7 9-4-2-7-5-7-9V6z" /></svg>
)
export const AlertCircle = ({ size, className }: P) => (
  <svg {...stroke(size, className)} strokeWidth={1.7}><circle cx="12" cy="12" r="9" /><path d="M12 8v4.5M12 16h.01" /></svg>
)
export const Chevron = ({ size = 14, className }: P) => (
  <svg {...stroke(size, className)}><path d="M9 6l6 6-6 6" /></svg>
)
export const Link2 = ({ size, className }: P) => (
  <svg {...stroke(size, className)}><circle cx="8" cy="12" r="3.2" /><circle cx="16" cy="12" r="3.2" /><path d="M11 12h2" /></svg>
)
export const CheckShield = ({ size, className }: P) => (
  <svg {...stroke(size, className)}><path d="M12 3l7 3v6c0 4-3 7-7 9-4-2-7-5-7-9V6z" /><path d="M9 12l2 2 4-4" /></svg>
)
