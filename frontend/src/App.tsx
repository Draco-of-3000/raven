import { useEffect, useMemo, useRef, useState } from 'react'
import { api, onEvent, onFileDrop, offFileDrop, humanBytes, etaText, cleanText, Status, Config, ReceiverInfo, IncomingFile } from './api'
import { RavenMark, Gear, Monitor, ArrowUp, ArrowDown, FolderGlyph, Chevron } from './icons'
import { Onboarding, Settings, AcceptPrompt, SendPicker, PairingDialog } from './screens'
import type { PairedDevice } from './api'

type Dir = 'send' | 'receive'
// Xfer tracks one transfer session. For folder/multi-file sessions, totalBytes is
// the sum across all files and bytesBefore is the bytes completed in prior files,
// so overall progress = (bytesBefore + current file bytes) / totalBytes.
interface Xfer { id: string; dir: Dir; peer: string; index: number; total: number; name: string; size: number; bytes: number; bps: number; totalBytes: number; bytesBefore: number; waiting?: boolean; done: boolean }
interface HistoryRow { key: string; dir: Dir; name: string; peer: string; size: number; count: number; folder: boolean; path: string; children?: ChildFile[]; verified: boolean; error: string; ts: number }
interface Incoming { id: string; peer: string; files: IncomingFile[]; totalSize: number }
interface Toast { id: number; text: string }
interface SessionFile { name: string; size: number; verified: boolean; error: string; savedTo: string }
interface SessionAcc { dir: Dir; files: SessionFile[] }
interface ChildFile { name: string; size: number }
interface CollapsedItem { label: string; folder: boolean; count: number; size: number; path: string; verified: boolean; error: string; children: ChildFile[] }

// folderRootPath derives the on-disk folder root from a saved file path and its
// folder-relative name. e.g. savedTo "/Users/me/Downloads/Raven/photos/trip/a.jpg"
// with name "photos/trip/a.jpg" -> "/Users/me/Downloads/Raven/photos".
function folderRootPath(savedTo: string, name: string): string {
  if (!savedTo) return ''
  const root = name.split('/')[0]
  const sep = savedTo.includes('\\') ? '\\' : '/'
  const idx = savedTo.lastIndexOf(sep + root + sep)
  if (idx >= 0) return savedTo.slice(0, idx + sep.length + root.length)
  return savedTo
}

// collapseItems groups a session's files by top-level folder. A file name like
// "photos/trip/a.jpg" collapses under "photos"; a bare "report.pdf" stays alone.
function collapseItems(files: SessionFile[]): CollapsedItem[] {
  const folders = new Map<string, CollapsedItem>()
  const out: CollapsedItem[] = []
  for (const f of files) {
    const slash = f.name.indexOf('/')
    if (slash > 0) {
      const root = f.name.slice(0, slash)
      let g = folders.get(root)
      if (!g) { g = { label: root, folder: true, count: 0, size: 0, path: folderRootPath(f.savedTo, f.name), verified: true, error: '', children: [] }; folders.set(root, g); out.push(g) }
      g.count += 1
      g.size += f.size
      g.children.push({ name: f.name.slice(slash + 1), size: f.size }) // path within the folder
      if (!f.verified) g.verified = false
      if (f.error && !g.error) g.error = f.error
    } else {
      out.push({ label: f.name, folder: false, count: 1, size: f.size, path: f.savedTo, verified: f.verified, error: f.error, children: [] })
    }
  }
  return out
}

const HISTORY_KEY = 'raven.history'
const ONBOARDED_KEY = 'raven.onboarded'

function applyTheme(theme: string) {
  const dark = theme === 'graphite' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.setAttribute('data-theme', dark ? 'graphite' : 'parchment')
}

function loadHistory(): HistoryRow[] {
  try { return JSON.parse(localStorage.getItem(HISTORY_KEY) || '[]') } catch { return [] }
}

export default function App() {
  const [onboarded, setOnboarded] = useState(localStorage.getItem(ONBOARDED_KEY) === '1')
  const [status, setStatus] = useState<Status | null>(null)
  const [config, setConfig] = useState<Config | null>(null)
  const [view, setView] = useState<'main' | 'settings'>('main')
  const [devices, setDevices] = useState<ReceiverInfo[]>([])
  const [discovering, setDiscovering] = useState(false)
  const [xfers, setXfers] = useState<Record<string, Xfer>>({})
  const [history, setHistory] = useState<HistoryRow[]>(loadHistory())
  const [incoming, setIncoming] = useState<Incoming | null>(null)
  const [sendPaths, setSendPaths] = useState<string[] | null>(null)
  const [filter, setFilter] = useState<'week' | 'lastweek' | 'month' | 'all'>('week')
  const [search, setSearch] = useState('')
  const [maskNames, setMaskNames] = useState(localStorage.getItem('raven.maskNames') === '1')
  const [missingPaths, setMissingPaths] = useState<Record<string, boolean>>({}) // path -> true if gone
  const [toasts, setToasts] = useState<Toast[]>([])
  const [fmName, setFmName] = useState('Files')
  const [pairing, setPairing] = useState<{ id: string; peer: string; code: string; incoming: boolean; waiting?: boolean } | null>(null)
  // Remembers a send that was interrupted to pair first, so we can resume it
  // automatically once pairing succeeds, instead of making the user start over.
  const pendingSend = useRef<{ target: string; paths: string[] } | null>(null)
  const [paired, setPaired] = useState<PairedDevice[]>([])

  const devicesRef = useRef<ReceiverInfo[]>([])
  devicesRef.current = devices
  const xfersRef = useRef<Record<string, Xfer>>({})
  xfersRef.current = xfers
  const sessionAcc = useRef<Record<string, SessionAcc>>({})
  const isWindows = useMemo(() => navigator.userAgent.includes('Windows'), [])

  const pushToast = (text: string) => {
    const id = Date.now() + Math.floor(Math.random() * 1000)
    setToasts((t) => [...t, { id, text }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 3600)
  }
  // The backend now sends the paired device's friendly name as the peer. Fall
  // back to mapping/trimming only if we still got an address (host:port or IP).
  const displayPeer = (peer: string) => {
    if (!peer) return 'device'
    const d = devicesRef.current.find((x) => x.addr === peer)
    if (d) return cleanText(d.name)
    const looksLikeAddr = /^\d{1,3}(\.\d{1,3}){3}(:\d+)?$/.test(peer) || /:\d+$/.test(peer)
    return cleanText(looksLikeAddr ? peer.split(':')[0] : peer)
  }
  const addHistory = (row: HistoryRow) => setHistory((h) => {
    const next = [row, ...h].slice(0, 200)
    localStorage.setItem(HISTORY_KEY, JSON.stringify(next))
    return next
  })

  function refreshDevices() {
    setDiscovering(true)
    api.listReceivers().then((d) => setDevices(d)).catch(() => {}).finally(() => setDiscovering(false))
  }

  useEffect(() => {
    api.getStatus().then(setStatus).catch(() => {})
    api.getConfig().then((c) => { setConfig(c); applyTheme(c.theme) }).catch(() => {})
    api.fileManagerName().then(setFmName).catch(() => {})
    api.listPaired().then(setPaired).catch(() => {})
    refreshDevices()

    const offs = [
      onEvent('receiver:status', (s: any) => setStatus(s)),
      onEvent('receiver:error', (e: any) => pushToast(`Receiver error: ${e?.message || 'unknown'}`)),
      onEvent('files:dropped', (paths: any) => { if (Array.isArray(paths) && paths.length) { setSendPaths(paths); refreshDevices() } }),
      onEvent('incoming:request', (r: any) => setIncoming(r as Incoming)),
      onEvent('incoming:timeout', (d: any) => setIncoming((prev) => (prev && prev.id === d?.id ? null : prev))),
      onEvent('incoming:auto', (d: any) => pushToast(`Receiving from ${cleanText(d?.peer || 'device')}`)),
      onEvent('pairing:code', (d: any) => {
        setSendPaths(null) // close the send picker so the code is visible on the sender
        setPairing({ id: d.id, peer: d.peer, code: d.code, incoming: !!d.incoming })
      }),
      onEvent('pairing:done', (d: any) => {
        setPairing(null)
        const peer = cleanText(d?.peer || 'device')
        if (d?.ok) {
          api.listPaired().then(setPaired).catch(() => {})
          pushToast(`Paired with ${peer}`)
          // Resume an interrupted send now that the device is trusted.
          const ps = pendingSend.current
          pendingSend.current = null
          if (ps) {
            pushToast(`Sending to ${peer}…`)
            api.sendFiles(ps.target, ps.paths).catch((err) => pushToast(String(err)))
          }
        } else {
          pendingSend.current = null
          pushToast(`Pairing with ${peer} was not completed`)
        }
      }),
      onEvent('pairing:error', (d: any) => { setPairing(null); pendingSend.current = null; pushToast(`Pairing failed: ${d?.message || 'unknown'}`) }),
      // Sender is blocked waiting for the receiver to accept; show a waiting card.
      onEvent('transfer:waiting', (d: any) => setXfers((m) => ({ ...m, [d.id]: { ...(m[d.id] || emptyXfer({ id: d.id, dir: 'send' })), peer: d.peer, waiting: true } }))),
      onEvent('transfer:start', (d: any) => setXfers((m) => ({ ...m, [d.id]: { id: d.id, dir: d.dir, peer: d.peer, index: 0, total: d.fileCount, name: '', size: 0, bytes: 0, bps: 0, totalBytes: d.totalBytes || 0, bytesBefore: 0, waiting: false, done: false } }))),
      onEvent('file:start', (d: any) => setXfers((m) => {
        const prev = m[d.id] || emptyXfer(d)
        // Roll the just-finished file's full size into bytesBefore so the
        // overall (folder) progress bar advances continuously, not per-file.
        const bytesBefore = prev.index > 0 ? prev.bytesBefore + prev.size : 0
        return { ...m, [d.id]: { ...prev, index: d.index, total: d.total, name: d.name, size: d.size, bytes: 0, bytesBefore } }
      })),
      onEvent('file:progress', (d: any) => setXfers((m) => (m[d.id] ? { ...m, [d.id]: { ...m[d.id], bytes: d.bytes, size: d.size, bps: d.bytesPerSec, name: d.name, index: d.index, total: d.total } } : m))),
      onEvent('file:done', (d: any) => {
        // Accumulate per-session; we collapse folders into single rows at transfer:end.
        const acc = sessionAcc.current[d.id] || { dir: d.dir, files: [] }
        acc.files.push({ name: d.name, size: d.size, verified: !!d.verified, error: d.error || '', savedTo: d.savedTo || '' })
        sessionAcc.current[d.id] = acc
      }),
      onEvent('transfer:end', (d: any) => {
        const peer = displayPeer((xfersRef.current[d.id]?.peer) || '')
        const acc = sessionAcc.current[d.id]
        delete sessionAcc.current[d.id]
        setXfers((m) => { const n = { ...m }; delete n[d.id]; return n })
        if (acc && acc.files.length) {
          const items = collapseItems(acc.files)
          const now = Date.now()
          items.forEach((it, i) => addHistory({
            key: `${d.id}-${i}-${now}`, dir: acc.dir, name: it.label, peer,
            size: it.size, count: it.count, folder: it.folder, path: it.path,
            children: it.folder ? it.children : undefined,
            verified: it.verified, error: it.error, ts: now,
          }))
          if (acc.dir === 'receive') {
            const ok = items.filter((it) => !it.error)
            if (ok.length === 1) pushToast(`Received ${ok[0].label}`)
            else if (ok.length > 1) pushToast(`Received ${ok.length} items`)
            const failed = items.filter((it) => it.error)
            if (failed.length) pushToast(`${failed.length} item${failed.length > 1 ? 's' : ''} failed`)
          }
        }
        // A cancel surfaces as a context-canceled / closed-conn error; the user
        // already knows they cancelled, so don't show a scary failure toast.
        const cancelled = /context canceled|use of closed|canceled|EOF/i.test(d.error || '')
        if (d.error && d.dir === 'send' && !cancelled) pushToast(`Send failed: ${d.error}`)
      }),
    ]

    // Frontend file-drop: the documented primary path. Fires with absolute paths
    // when files/folders are dropped anywhere on the window.
    try {
      onFileDrop((_x, _y, paths) => {
        if (paths && paths.length) { setSendPaths(paths); refreshDevices() }
      }, false)
    } catch {}

    return () => {
      offs.forEach((off) => { try { off && off() } catch {} })
      try { offFileDrop() } catch {}
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (config?.theme !== 'system') return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const h = () => applyTheme('system')
    mq.addEventListener('change', h)
    return () => mq.removeEventListener('change', h)
  }, [config?.theme])

  // Check which received files still exist on disk, so history can strike through
  // ones that were moved or deleted. Re-runs when the set of received paths changes.
  const receivedPaths = useMemo(
    () => Array.from(new Set(history.filter((r) => r.dir === 'receive' && r.path).map((r) => r.path))),
    [history],
  )
  useEffect(() => {
    if (receivedPaths.length === 0) { setMissingPaths({}); return }
    api.checkPaths(receivedPaths).then((exists) => {
      const missing: Record<string, boolean> = {}
      for (const p of receivedPaths) if (exists[p] === false) missing[p] = true
      setMissingPaths(missing)
    }).catch(() => {})
  }, [receivedPaths])

  const toggleMask = () => setMaskNames((m) => { const v = !m; localStorage.setItem('raven.maskNames', v ? '1' : '0'); return v })

  const onChooseDir = () => api.chooseSaveDir().then((dir) => { if (dir) setConfig((c) => (c ? { ...c, saveDir: dir } : c)) }).catch(() => {})
  const onSetName = (n: string) => { if (n) { api.setDeviceName(n); setConfig((c) => (c ? { ...c, deviceName: n } : c)) } }
  const onSetTheme = (t: string) => { api.setTheme(t); applyTheme(t); setConfig((c) => (c ? { ...c, theme: t } : c)) }
  const onSetPort = (p: number) => { api.setPort(p); setConfig((c) => (c ? { ...c, port: p } : c)) }
  // Pair from the send picker: remember any queued files so they send right after.
  const startPair = (target: string) => {
    const paths = sendPaths || []
    if (paths.length) { pendingSend.current = { target, paths }; setSendPaths(null) }
    api.startPairing(target).catch((e) => { pendingSend.current = null; pushToast(String(e)) })
  }
  const onForget = (fp: string) => api.forgetDevice(fp).then(() => api.listPaired().then(setPaired)).catch(() => {})
  const onSetAutoAccept = (on: boolean) => { api.setAutoAcceptPaired(on); setStatus((s) => (s ? { ...s, autoAcceptPaired: on } : s)) }

  const startSend = (target: string) => {
    const paths = sendPaths || []
    setSendPaths(null)
    if (!paths.length) return
    api.sendFiles(target, paths).catch((err) => {
      const msg = String(err)
      // If the device isn't paired yet, remember this send and start pairing;
      // pairing:done will resume it automatically so the user isn't sent back
      // to the start.
      if (/not paired/i.test(msg)) {
        pendingSend.current = { target, paths }
        pushToast('Pair this device first, then your files will send automatically')
        api.startPairing(target).catch((e) => { pendingSend.current = null; pushToast(String(e)) })
      } else {
        pushToast(msg)
      }
    })
  }
  // Merge newly picked paths into the current selection (deduped), opening the picker.
  const addPaths = (paths: string[]) => {
    if (!paths.length) return
    setSendPaths((prev) => Array.from(new Set([...(prev || []), ...paths])))
    refreshDevices()
  }
  const removePath = (p: string) => setSendPaths((prev) => {
    const next = (prev || []).filter((x) => x !== p)
    return next.length ? next : null
  })
  const pickFiles = () => api.pickFiles().then(addPaths).catch(() => {})
  const pickFolder = () => api.pickFolder().then(addPaths).catch(() => {})
  const onCancelTransfer = (id: string) => {
    api.cancelTransfer(id).catch(() => {})
    setXfers((m) => { const n = { ...m }; delete n[id]; return n }) // clear immediately for snappy UI
    pushToast('Transfer cancelled')
  }

  if (!onboarded) {
    return <Onboarding config={config || blankConfig()} isWindows={isWindows} onChooseDir={onChooseDir}
      onFinish={(name) => { onSetName(name); localStorage.setItem(ONBOARDED_KEY, '1'); setOnboarded(true) }} />
  }

  const active = Object.values(xfers).find((x) => !x.done)

  return (
    <div className="app">
      <div className="titlebar">
        {/* Mac hides the native title, so we show the wordmark. Windows already
            shows "Raven" in its own title bar, so we omit the duplicate text. */}
        <span className="wm"><RavenMark size={19} />{!isWindows && <span>Raven</span>}</span>
        <span className="gear" onClick={() => setView(view === 'settings' ? 'main' : 'settings')} title="Settings">
          {view === 'settings' ? <span style={{ fontSize: 12, fontWeight: 600 }}>Done</span> : <Gear size={18} />}
        </span>
      </div>

      {view === 'settings' && config ? (
        <Settings config={config} status={status} onSetName={onSetName} onSetTheme={onSetTheme} onSetPort={onSetPort}
          onChooseDir={onChooseDir} paired={paired} onForget={onForget} onSetAutoAccept={onSetAutoAccept} />
      ) : (
        <>
          <div className="main">
            <div className="stage">
              {active ? <Flight x={active} displayPeer={displayPeer} onCancel={onCancelTransfer} /> : (
                <>
                  <RavenMark size={60} className="mark" />
                  <h2>Drop files or a folder to send</h2>
                  <p>Sent directly to your paired device, encrypted along the way.</p>
                  <span className="pickrow">
                    <span className="pick" onClick={pickFiles}>Choose files</span>
                    <span className="pickdot">·</span>
                    <span className="pick" onClick={pickFolder}>Choose a folder</span>
                  </span>
                </>
              )}
              {incoming && config && <AcceptPrompt incoming={incoming} saveDir={config.saveDir}
                onRespond={(id, ok) => { api.respondToIncoming(id, ok); setIncoming(null) }} />}
            </div>
            <Rail status={status} devices={devices} discovering={discovering} onRefresh={refreshDevices} onPick={pickFiles} />
          </div>
          <History rows={history} filter={filter} setFilter={setFilter}
            search={search} setSearch={setSearch} maskNames={maskNames} toggleMask={toggleMask}
            missingPaths={missingPaths}
            onReveal={(p) => api.reveal(p).catch((e) => pushToast(String(e)))} fmName={fmName} />
        </>
      )}

      {sendPaths && (
        <SendPicker paths={sendPaths} devices={devices} discovering={discovering} pairedFPs={paired.map((d) => d.name)}
          onRefresh={refreshDevices} onClose={() => setSendPaths(null)} onSend={(target) => startSend(target)}
          onPair={startPair} onAddFiles={pickFiles} onAddFolder={pickFolder} onRemove={removePath} />
      )}

      {pairing && <PairingDialog pairing={pairing} onRespond={(id, ok) => {
        api.confirmPairing(id, ok)
        if (ok) setPairing((p) => (p ? { ...p, waiting: true } : p)) // show "waiting for other device"
        else setPairing(null)
      }} />}

      <div className="toasts">
        {toasts.map((t) => <div className="toast" key={t.id}><span className="tdot" />{t.text}</div>)}
      </div>
    </div>
  )
}

function emptyXfer(d: any): Xfer { return { id: d.id, dir: d.dir || 'receive', peer: d.peer || '', index: 0, total: d.total || 1, name: '', size: 0, bytes: 0, bps: 0, totalBytes: 0, bytesBefore: 0, done: false } }
function blankConfig(): Config { return { deviceName: 'This device', saveDir: '~/Downloads/Raven', theme: 'system', port: 51888, autoAcceptPaired: false } }

function Flight({ x, displayPeer, onCancel }: { x: Xfer; displayPeer: (p: string) => string; onCancel: (id: string) => void }) {
  const peer = displayPeer(x.peer)

  // Waiting for the receiver to accept: no progress yet, just a clear status.
  if (x.waiting) {
    return (
      <div className="waitcard">
        <RavenMark size={48} className="mark" />
        <div className="spinrow"><span className="spinner" />Waiting for {peer} to accept…</div>
        <p className="waithint">Your files will start sending as soon as they accept.</p>
        <button className="btn ghost sm" onClick={() => onCancel(x.id)}>Cancel</button>
      </div>
    )
  }

  const left = x.dir === 'send' ? 'You' : peer
  const right = x.dir === 'send' ? peer : 'You'

  // A multi-file/folder session tracks progress across ALL bytes, so the bar and
  // the flying raven advance continuously instead of resetting per file.
  const multi = x.total > 1 && x.totalBytes > 0
  const overallBytes = multi ? x.bytesBefore + x.bytes : x.bytes
  const overallTotal = multi ? x.totalBytes : x.size
  const pct = overallTotal > 0 ? Math.min(100, (overallBytes / overallTotal) * 100) : 0
  const eta = etaText(overallBytes, overallTotal, x.bps)

  // Folder file names arrive as "folder/sub/file.ext"; show the folder as the
  // title and the current file beneath it so a big folder reads as one transfer.
  const slash = x.name.indexOf('/')
  const isFolder = slash > 0
  const title = cleanText(isFolder ? x.name.slice(0, slash) : (x.name || (x.dir === 'send' ? 'Sending…' : 'Receiving…')))
  const subPath = isFolder ? cleanText(x.name.slice(slash + 1)) : ''
  return (
    <>
      <div className="flightzone">
        <div className="endpoints">
          <span className="endp l"><span className="pin" />{left}</span>
          <span className="endp r">{right}<span className="pin" /></span>
        </div>
        <div className="flighttrack">
          <span className="flyer" style={{ left: `${pct}%` }}>
            <span className="bob"><RavenMark size={28} className="bird" /></span>
          </span>
        </div>
      </div>
      <div className="xfer">
        <div className="fname">{isFolder && <span className="ffolder"><FolderGlyph size={15} /></span>}{title}</div>
        {subPath && <div className="fsub">{subPath}</div>}
        <div className="track"><i style={{ width: `${pct}%` }} /></div>
        <div className="xmeta">
          {x.dir === 'send' ? 'to' : 'from'} {peer}<span className="sp">·</span>
          <span className="mono">{humanBytes(overallTotal)}</span><span className="sp">·</span>
          <span className="mono">{humanBytes(x.bps)}/s</span>
          {x.total > 1 && <><span className="sp">·</span>{x.index} of {x.total} files</>}
          {eta && <><span className="sp">·</span>{eta}</>}
        </div>
        <button className="btn ghost sm xcancel" onClick={() => onCancel(x.id)}>Cancel</button>
      </div>
    </>
  )
}

function Rail({ status, devices, discovering, onRefresh, onPick }: {
  status: Status | null; devices: ReceiverInfo[]; discovering: boolean; onRefresh: () => void; onPick: (addr: string) => void
}) {
  return (
    <div className="rail">
      <div>
        <div className="lbl">This device</div>
        <div className="dev">{status?.deviceName || '…'}</div>
        <div className="addr">{status?.ip ? `${status.ip} · ${status.port}` : '…'}</div>
        <div className="listen">
          {status?.listening ? <><span className="dotok" />Listening</> : <><span className="dotoff" />Not listening</>}
        </div>
      </div>
      <div className="divider" />
      <div>
        <div className="lbl">Devices nearby {discovering ? '·' : ''}</div>
        {devices.length === 0 && <div className="muted">No devices found yet.</div>}
        {devices.map((d) => (
          <div className="nrow" key={d.addr} onClick={() => onPick(d.addr)}>
            <span className="ic"><Monitor size={18} /></span>
            <span className="dn">{cleanText(d.name)}</span><span className="da">{d.addr.split(':')[0]}</span>
          </div>
        ))}
        <div className="addln" onClick={onRefresh}>Refresh</div>
      </div>
    </div>
  )
}

const MASK = '••••••••'
function maskLabel(s: string) { return s ? MASK : s }

function History({ rows, filter, setFilter, search, setSearch, maskNames, toggleMask, missingPaths, onReveal, fmName }: {
  rows: HistoryRow[]; filter: 'week' | 'lastweek' | 'month' | 'all'; setFilter: (f: any) => void
  search: string; setSearch: (s: string) => void; maskNames: boolean; toggleMask: () => void
  missingPaths: Record<string, boolean>; onReveal: (path: string) => void; fmName: string
}) {
  // Search matches on filename or peer. When names are masked, searching by
  // filename is intentionally disabled (you can still match by device).
  const groups = useMemo(() => groupHistory(rows, filter, search, maskNames), [rows, filter, search, maskNames])
  const filters: [string, string][] = [['week', 'This week'], ['lastweek', 'Last week'], ['month', 'This month'], ['all', 'All time']]
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const [collapsedDays, setCollapsedDays] = useState<Record<string, boolean>>({})
  const toggle = (k: string) => setExpanded((e) => ({ ...e, [k]: !e[k] }))
  const toggleDay = (label: string) => setCollapsedDays((d) => ({ ...d, [label]: !d[label] }))
  return (
    <div className="history">
      <div className="hhead">
        <span className="ht">History</span>
        <div className="hsearch">
          <input placeholder={maskNames ? 'Search devices…' : 'Search files or devices…'} value={search} onChange={(e) => setSearch(e.target.value)} />
          {search && <span className="hclear" onClick={() => setSearch('')}>×</span>}
        </div>
        <span className={'maskbtn' + (maskNames ? ' on' : '')} onClick={toggleMask} title={maskNames ? 'Show file names' : 'Hide file names'}>
          {maskNames ? 'File names hidden' : 'Hide file names'}
        </span>
        <div className="filters">
          {filters.map(([v, l]) => <button key={v} className={'fseg' + (filter === v ? ' on' : '')} onClick={() => setFilter(v)}>{l}</button>)}
        </div>
      </div>
      <div className="hlist">
        {groups.length === 0 && <div className="empty">{search ? 'No matches.' : 'No transfers in this range.'}</div>}
        {groups.map((g) => {
          const dayCollapsed = !!collapsedDays[g.label]
          return (
            <div className="daygrp" key={g.label}>
              <div className="dh" onClick={() => toggleDay(g.label)}>
                <span className={'dhchev' + (dayCollapsed ? '' : ' open')}><Chevron size={12} /></span>
                {g.label}<span className="dhcount">{g.rows.length}</span>
              </div>
              {!dayCollapsed && g.rows.map((r) => {
                const gone = r.dir === 'receive' && !!r.path && !!missingPaths[r.path]
                const canReveal = !!r.path && !r.error && !gone
                const hasChildren = r.folder && !!r.children && r.children.length > 0
                const isOpen = !!expanded[r.key]
                const shownName = maskNames ? maskLabel(r.name) : cleanText(r.name)
                const label = shownName + (r.folder ? ` (${r.count} ${r.count === 1 ? 'file' : 'files'})` : '')
                return (
                  <div key={r.key}>
                    <div className="hrow">
                      <span className={'dir ' + (r.dir === 'send' ? 'sent' : 'recv')}>{r.dir === 'send' ? <ArrowUp size={16} /> : <ArrowDown size={16} />}</span>
                      {hasChildren
                        ? <span className={'hexpand' + (isOpen ? ' open' : '')} onClick={() => toggle(r.key)} title={isOpen ? 'Hide files' : 'Show files'}><Chevron size={13} /></span>
                        : r.folder && <span className="hfolder"><FolderGlyph size={14} /></span>}
                      {canReveal
                        ? <span className={'fn link' + (gone ? ' gone' : '')} title={`Show in ${fmName}`} onClick={() => onReveal(r.path)}>{label}</span>
                        : <span className={'fn' + (gone ? ' gone' : '')} title={gone ? 'Moved or deleted from its saved location' : undefined}>{label}</span>}
                      <span className="peer">{r.dir === 'send' ? 'to' : 'from'} {cleanText(r.peer)}</span>
                      <span className="sz">{humanBytes(r.size)}</span>
                      {r.error ? <span className="er">failed</span> : gone ? <span className="gonetag">moved</span> : <span className="vf">✓</span>}
                      <span className="tm">{timeLabel(r.ts)}</span>
                    </div>
                    {hasChildren && isOpen && (
                      <div className="hchildren">
                        {r.children!.map((c, i) => (
                          <div className="hchild" key={i}>
                            <span className="cfn">{maskNames ? maskLabel(c.name) : cleanText(c.name)}</span>
                            <span className="csz">{humanBytes(c.size)}</span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function startOfDay(d: Date) { return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime() }
function startOfWeek(now: Date) { const d = new Date(now); const day = (d.getDay() + 6) % 7; return startOfDay(new Date(d.getFullYear(), d.getMonth(), d.getDate() - day)) }
function timeLabel(ts: number) { return new Date(ts).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }) }
function dayLabel(ts: number, now: Date) {
  const sToday = startOfDay(now)
  const s = startOfDay(new Date(ts))
  if (s === sToday) return 'Today'
  if (s === sToday - 86400000) return 'Yesterday'
  return new Date(ts).toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })
}
function groupHistory(rows: HistoryRow[], filter: string, search = '', maskNames = false) {
  const now = new Date()
  const sWeek = startOfWeek(now)
  const sLastWeek = sWeek - 7 * 86400000
  const sMonth = new Date(now.getFullYear(), now.getMonth(), 1).getTime()
  const keep = (ts: number) => {
    if (filter === 'week') return ts >= sWeek
    if (filter === 'lastweek') return ts >= sLastWeek && ts < sWeek
    if (filter === 'month') return ts >= sMonth
    return true
  }
  const q = search.trim().toLowerCase()
  const matches = (r: HistoryRow) => {
    if (!q) return true
    // When names are masked, don't leak them via search; match device only.
    const hay = (maskNames ? r.peer : `${r.name} ${r.peer}`).toLowerCase()
    return hay.includes(q)
  }
  const out: { label: string; rows: HistoryRow[] }[] = []
  for (const r of rows.filter((x) => keep(x.ts) && matches(x))) {
    const label = dayLabel(r.ts, now)
    let g = out.find((x) => x.label === label)
    if (!g) { g = { label, rows: [] }; out.push(g) }
    g.rows.push(r)
  }
  return out
}
