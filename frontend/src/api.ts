import * as Backend from '../wailsjs/go/main/App'
import { EventsOn, OnFileDrop, OnFileDropOff } from '../wailsjs/runtime/runtime'

export interface Status { listening: boolean; ip: string; port: number; saveDir: string; deviceName: string; theme: string; fingerprint: string; shortFingerprint: string; autoAcceptPaired: boolean }
export interface Config { deviceName: string; saveDir: string; theme: string; port: number; autoAcceptPaired: boolean }
export interface ReceiverInfo { name: string; addr: string }
export interface IncomingFile { name: string; size: number; rel?: string }
export interface PairedDevice { name: string; fingerprint: string; pairedAt: number; lastSeen: number }

export const api = {
  getStatus: async (): Promise<Status> => (await Backend.GetStatus()) as unknown as Status,
  getConfig: async (): Promise<Config> => (await Backend.GetConfig()) as unknown as Config,
  listReceivers: async (): Promise<ReceiverInfo[]> => ((await Backend.ListReceivers()) as unknown as ReceiverInfo[]) ?? [],
  sendFiles: (target: string, paths: string[]): Promise<void> => Backend.SendFiles(target, paths),
  pickFiles: async (): Promise<string[]> => ((await Backend.PickFilesToSend()) as unknown as string[]) ?? [],
  pickFolder: async (): Promise<string[]> => ((await Backend.PickFolderToSend()) as unknown as string[]) ?? [],
  chooseSaveDir: (): Promise<string> => Backend.ChooseSaveDir(),
  setDeviceName: (n: string): Promise<void> => Backend.SetDeviceName(n),
  setTheme: (t: string): Promise<void> => Backend.SetTheme(t),
  setPort: (p: number): Promise<void> => Backend.SetPort(p),
  respondToIncoming: (id: string, accept: boolean): Promise<void> => Backend.RespondToIncoming(id, accept),
  reveal: (path: string): Promise<void> => Backend.RevealInFileManager(path),
  fileManagerName: async (): Promise<string> => (await Backend.FileManagerName()) || 'Files',
  startPairing: (target: string): Promise<void> => Backend.StartPairing(target),
  confirmPairing: (id: string, accept: boolean): Promise<void> => Backend.ConfirmPairing(id, accept),
  listPaired: async (): Promise<PairedDevice[]> => ((await Backend.ListPairedDevices()) as unknown as PairedDevice[]) ?? [],
  forgetDevice: (fingerprint: string): Promise<void> => Backend.ForgetDevice(fingerprint),
  setAutoAcceptPaired: (on: boolean): Promise<void> => Backend.SetAutoAcceptPaired(on),
  cancelTransfer: (id: string): Promise<void> => Backend.CancelTransfer(id),
  checkPaths: async (paths: string[]): Promise<Record<string, boolean>> => ((await Backend.CheckPaths(paths)) as unknown as Record<string, boolean>) ?? {},
}

export const onEvent = EventsOn
// Frontend file-drop API (the documented primary path). useDropTarget=false: we
// handle the whole window ourselves and don't need Wails' CSS-target tracking.
export const onFileDrop = OnFileDrop
export const offFileDrop = OnFileDropOff

export function humanBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const u = ['KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let v = n, i = -1
  do { v /= 1024; i++ } while (v >= 1024 && i < u.length - 1)
  return `${v.toFixed(1)} ${u[i]}`
}

export function fileName(p: string): string {
  const parts = p.split(/[\\/]/)
  return parts[parts.length - 1] || p
}

// cleanText removes control characters and Unicode bidi overrides/isolates from
// an untrusted string (peer device names, incoming file names) and caps length,
// so a crafted name cannot spoof an extension or break the layout. Defense in
// depth: the Go side also sanitizes on-disk names. Filtered by code point so no
// literal control characters live in this source file.
export function cleanText(s: string, max = 200): string {
  if (!s) return s
  let out = ''
  for (const ch of s) {
    const c = ch.codePointAt(0)!
    if (c < 0x20 || c === 0x7f) continue                         // C0 controls + DEL
    if (c >= 0x80 && c <= 0x9f) continue                         // C1 controls
    if (c >= 0x202a && c <= 0x202e) continue                     // bidi embeddings/overrides
    if (c >= 0x2066 && c <= 0x2069) continue                     // bidi isolates
    if (c === 0x200e || c === 0x200f || c === 0x061c) continue   // bidi marks
    out += ch
  }
  out = out.trim()
  return out.length > max ? out.slice(0, max - 1) + '…' : out
}

export function etaText(bytes: number, size: number, bps: number): string {
  if (bps <= 0 || bytes >= size) return ''
  const secs = Math.max(1, Math.round((size - bytes) / bps))
  if (secs < 60) return `about ${secs}s left`
  const m = Math.round(secs / 60)
  return `about ${m}m left`
}
