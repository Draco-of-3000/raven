import { useState } from 'react'
import { Config, Status, IncomingFile, ReceiverInfo, PairedDevice, humanBytes, fileName, cleanText } from './api'
import { RavenMark, Wifi, FileGlyph, FolderGlyph, ArrowDown, Monitor, Chevron, Link2, CheckShield } from './icons'

/* ---------------- Onboarding ---------------- */
export function Onboarding(props: {
  config: Config
  isWindows: boolean
  onChooseDir: () => void
  onFinish: (deviceName: string) => void
}) {
  const [step, setStep] = useState(0)
  const [name, setName] = useState(props.config.deviceName)
  const labels = ['Get started', 'Continue', 'Start using Raven']
  const net = props.isWindows
    ? 'Raven sends directly over Wi-Fi. Windows Firewall will ask whether to allow Raven on your network. Allow it on private networks so others can reach you.'
    : 'Raven sends directly over Wi-Fi. macOS will ask permission to find and connect to devices on your local network. Allow it so others can reach you.'

  const next = () => {
    if (step < 2) setStep(step + 1)
    else props.onFinish(name.trim() || props.config.deviceName)
  }

  return (
    <div className="app">
      <div className="titlebar"><span className="wm"><RavenMark size={19} /><span>Raven</span></span></div>
      <div className="onb">
        <div className="panelwrap">
          {step === 0 && (
            <div className="panel">
              <RavenMark size={60} className="mark" />
              <h2>Welcome to Raven</h2>
              <p className="lead">Send files straight between your devices on the same network. No cloud, no accounts.</p>
            </div>
          )}
          {step === 1 && (
            <div className="panel">
              <span className="eyebrow">Your device</span>
              <h2>How you'll appear nearby</h2>
              <div className="field">
                <label>Device name</label>
                <div className="input"><input value={name} onChange={(e) => setName(e.target.value)} /></div>
              </div>
              <div className="field">
                <label>Save received files to</label>
                <div className="folderrow">
                  <span className="pathv">{props.config.saveDir}</span>
                  <span className="change" onClick={props.onChooseDir}>Change</span>
                </div>
              </div>
            </div>
          )}
          {step === 2 && (
            <div className="panel">
              <span className="eyebrow">Almost ready</span>
              <h2>Stay reachable on your network</h2>
              <div className="net"><Wifi size={20} className="ni" /><p>{net}</p></div>
              <div className="ready">You'll listen as <b>{name || props.config.deviceName}</b></div>
            </div>
          )}
        </div>
        <div className="footerbar">
          <div className="dots">{[0, 1, 2].map((i) => <span key={i} className={'dot' + (i === step ? ' on' : '')} />)}</div>
          <div className="acts">
            {step > 0 && <button className="btn ghost sm" onClick={() => setStep(step - 1)}>Back</button>}
            <button className="btn primary sm" onClick={next}>{labels[step]}</button>
          </div>
        </div>
      </div>
    </div>
  )
}

// shortFP formats a hex fingerprint as the first 8 colon-separated byte groups.
function shortFP(fp: string): string {
  const up = (fp || '').toUpperCase().slice(0, 16)
  return (up.match(/.{1,2}/g) || []).join(':')
}

/* ---------------- Settings ---------------- */
export function Settings(props: {
  config: Config
  status: Status | null
  paired: PairedDevice[]
  onSetName: (n: string) => void
  onSetTheme: (t: string) => void
  onSetPort: (p: number) => void
  onChooseDir: () => void
  onForget: (fingerprint: string) => void
  onSetAutoAccept: (on: boolean) => void
}) {
  const [name, setName] = useState(props.config.deviceName)
  const [port, setPort] = useState(String(props.config.port))
  const themes = [['parchment', 'Parchment'], ['graphite', 'Graphite'], ['system', 'System']]
  return (
    <div className="settings">
      <div className="stitle">Settings</div>

      <div className="grp">
        <div className="lbl">Identity</div>
        <div className="srow">
          <div><div className="slab">Device name</div><div className="sdesc">How you appear to others nearby.</div></div>
          <div className="sctl"><div className="input" style={{ minWidth: 190 }}>
            <input value={name} onChange={(e) => setName(e.target.value)} onBlur={() => props.onSetName(name.trim())} />
          </div></div>
        </div>
      </div>

      <div className="grp">
        <div className="lbl">Files</div>
        <div className="srow">
          <div><div className="slab">Save received files to</div></div>
          <div className="sctl"><span className="addr">{props.config.saveDir}</span><span className="addln" onClick={props.onChooseDir}>Change</span></div>
        </div>
      </div>

      <div className="grp">
        <div className="lbl">Appearance</div>
        <div className="srow">
          <div><div className="slab">Theme</div></div>
          <div className="sctl">
            {themes.map(([v, l]) => (
              <button key={v} className={'seg' + (props.config.theme === v ? ' on' : '')} onClick={() => props.onSetTheme(v)}>{l}</button>
            ))}
          </div>
        </div>
      </div>

      <div className="grp">
        <div className="lbl">Security</div>
        <div className="srow">
          <div><div className="slab">This device's identity</div><div className="sdesc">Others verify this fingerprint when pairing with you.</div></div>
          <div className="sctl"><span className="addr">{props.status?.shortFingerprint || '…'}</span></div>
        </div>
        <div className="srow">
          <div><div className="slab">Auto-accept from paired devices</div><div className="sdesc">Skip the accept prompt for devices you've paired with.</div></div>
          <div className="sctl">
            <button className={'seg' + (!props.config.autoAcceptPaired ? ' on' : '')} onClick={() => props.onSetAutoAccept(false)}>Always ask</button>
            <button className={'seg' + (props.config.autoAcceptPaired ? ' on' : '')} onClick={() => props.onSetAutoAccept(true)}>Auto-accept</button>
          </div>
        </div>
      </div>

      <div className="grp">
        <div className="lbl">Paired devices {props.paired.length > 0 ? `(${props.paired.length})` : ''}</div>
        {props.paired.length === 0 && <div className="muted" style={{ padding: '8px 0' }}>No paired devices yet. Drag in a file, then choose Pair on the device you want.</div>}
        {props.paired.map((d) => (
          <div className="srow" key={d.fingerprint}>
            <div><div className="slab">{cleanText(d.name)}</div><div className="sdesc" style={{ fontFamily: 'var(--mono)' }}>{shortFP(d.fingerprint)}</div></div>
            <div className="sctl"><button className="rowbtn forget" onClick={() => props.onForget(d.fingerprint)}>Unpair</button></div>
          </div>
        ))}
      </div>

      <div className="grp">
        <div className="lbl">About</div>
        <div className="srow">
          <div><div className="slab">Raven 1.0</div><div className="sdesc">Direct, encrypted, local file transfer.</div></div>
          <div className="sctl"><span className="addln">Acknowledgements</span></div>
        </div>
      </div>

      <details className="adv">
        <summary>Advanced</summary>
        <div className="srow">
          <div><div className="slab">Listen port</div><div className="sdesc">The network door Raven listens on. Rarely changed; must match on both devices if you change it.</div></div>
          <div className="sctl"><div className="input" style={{ minWidth: 90 }}>
            <input value={port} onChange={(e) => setPort(e.target.value)} onBlur={() => { const p = parseInt(port, 10); if (p > 0) props.onSetPort(p) }} />
          </div></div>
        </div>
      </details>
    </div>
  )
}

/* ---------------- Accept prompt ---------------- */
interface AcceptItem { folder: boolean; name: string; size: number; count: number }

// Group incoming files: those sharing a top-level folder (from rel) collapse
// into one folder row; loose files stay individual.
function groupIncoming(files: IncomingFile[]): AcceptItem[] {
  const folders = new Map<string, AcceptItem>()
  const items: AcceptItem[] = []
  for (const f of files) {
    const root = f.rel ? f.rel.split('/')[0] : ''
    if (root) {
      let g = folders.get(root)
      if (!g) { g = { folder: true, name: root, size: 0, count: 0 }; folders.set(root, g); items.push(g) }
      g.size += f.size
      g.count += 1
    } else {
      items.push({ folder: false, name: f.name, size: f.size, count: 1 })
    }
  }
  return items
}

export function AcceptPrompt(props: {
  incoming: { id: string; peer: string; files: IncomingFile[]; totalSize: number }
  saveDir: string
  onRespond: (id: string, accept: boolean) => void
}) {
  const { incoming } = props
  const items = groupIncoming(incoming.files)
  const label = items.length === 1 ? '1 item' : `${items.length} items`
  return (
    <div className="overlay">
      <span className="inbound"><ArrowDown size={14} />Incoming</span>
      <h2>{incoming.peer.split(':')[0]} wants to send you {label}</h2>
      <div className="reqmeta">{humanBytes(incoming.totalSize)} total, from <span className="mono">{incoming.peer}</span></div>
      <div className="filelist">
        {items.map((it, i) => (
          <div className="frow" key={i}>
            <span className="fi">{it.folder ? <FolderGlyph size={16} /> : <FileGlyph size={16} />}</span>
            <span className="fn">{cleanText(it.name)}{it.folder ? `  (${it.count} ${it.count === 1 ? 'file' : 'files'})` : ''}</span>
            <span className="fs">{humanBytes(it.size)}</span>
          </div>
        ))}
      </div>
      <div className="actions">
        <button className="btn ghost" onClick={() => props.onRespond(incoming.id, false)}>Decline</button>
        <button className="btn primary" onClick={() => props.onRespond(incoming.id, true)}>Accept &amp; save</button>
      </div>
      <div className="saveline">Saves to <span className="mono">{props.saveDir}</span></div>
    </div>
  )
}

/* ---------------- Pairing dialog ---------------- */
// Shows the 6-digit Short Authentication String. The user MUST compare it with
// the code shown on the other device; matching codes prove there is no
// man-in-the-middle. This human comparison is the security of first pairing.
export function PairingDialog(props: {
  pairing: { id: string; peer: string; code: string; incoming: boolean; waiting?: boolean }
  onRespond: (id: string, accept: boolean) => void
}) {
  const { pairing } = props
  return (
    <div className="overlay pairing-overlay">
      <span className="inbound"><Link2 size={14} />Pairing</span>
      <h2>{pairing.incoming ? `${cleanText(pairing.peer)} wants to pair` : `Pair with ${cleanText(pairing.peer)}`}</h2>
      <p className="pairlead">Make sure this code is the same on both devices before you continue.</p>
      <div className="sascode">{pairing.code}</div>
      {pairing.waiting ? (
        <div className="pairwait"><span className="spinner" />Waiting for the other device to confirm…</div>
      ) : (
        <div className="actions">
          <button className="btn ghost" onClick={() => props.onRespond(pairing.id, false)}>Codes differ</button>
          <button className="btn primary" onClick={() => props.onRespond(pairing.id, true)}>Codes match</button>
        </div>
      )}
      <div className="saveline">Pairing lets these two devices send to each other securely.</div>
    </div>
  )
}

/* ---------------- Send picker ---------------- */
export function SendPicker(props: {
  paths: string[]
  devices: ReceiverInfo[]
  discovering: boolean
  pairedFPs: string[] // paired device names (best-effort match without fingerprint-in-discovery)
  onRefresh: () => void
  onSend: (target: string, name: string) => void
  onPair: (target: string) => void
  onAddFiles: () => void
  onAddFolder: () => void
  onRemove: (path: string) => void
  onClose: () => void
}) {
  const [manual, setManual] = useState('')
  const isFolder = (p: string) => !/\.[A-Za-z0-9]{1,8}$/.test(fileName(p)) // heuristic: no extension => folder
  const isPaired = (name: string) => props.pairedFPs.includes(name)
  return (
    <div className="scrim" onClick={props.onClose}>
      <div className="sheet" onClick={(e) => e.stopPropagation()}>
        <h3>Send {props.paths.length} {props.paths.length === 1 ? 'item' : 'items'}</h3>

        <div className="itemlist">
          {props.paths.map((p) => (
            <div className="itemrow" key={p}>
              <span className="ii">{isFolder(p) ? <FolderGlyph size={15} /> : <FileGlyph size={15} />}</span>
              <span className="iname">{fileName(p)}</span>
              <span className="irm" title="Remove" onClick={() => props.onRemove(p)}>×</span>
            </div>
          ))}
        </div>
        <div className="addrow">
          <span className="addbtn2" onClick={props.onAddFiles}>＋ Add files</span>
          <span className="addbtn2" onClick={props.onAddFolder}>＋ Add a folder</span>
        </div>

        <div className="lbl" style={{ marginTop: 14 }}>Send to {props.discovering ? '(searching…)' : ''}</div>
        <div className="picklist">
          {props.devices.length === 0 && <div className="muted">No devices found yet. Open Raven on the other device, or add it by address below.</div>}
          {props.devices.map((d) => {
            const pairedDev = isPaired(d.name)
            return (
              <div className="nrow" key={d.addr}>
                <span className="ic">{pairedDev ? <CheckShield size={18} /> : <Monitor size={18} />}</span>
                <span className="dn">{cleanText(d.name)}</span><span className="da">{d.addr}</span>
                {pairedDev
                  ? <button className="rowbtn" onClick={() => props.paths.length && props.onSend(d.addr, d.name)}>Send</button>
                  : <button className="rowbtn pair" onClick={() => props.onPair(d.addr)}>Pair</button>}
              </div>
            )
          })}
        </div>
        <div className="manualrow">
          <div className="input"><input placeholder="192.168.1.x" value={manual} onChange={(e) => setManual(e.target.value)} /></div>
          <button className="btn primary sm" disabled={!manual.trim() || !props.paths.length} onClick={() => props.onSend(manual.trim(), manual.trim())}>Send</button>
        </div>
      </div>
    </div>
  )
}
