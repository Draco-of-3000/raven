import React from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import './index.css'

// Note: we do NOT preventDefault on window drop here. Wails' own file-drop
// handling (frontend OnFileDrop) needs the native drop to reach it; intercepting
// it in the DOM swallows the event. The webview's "open as a tab" behavior is
// suppressed natively via DragAndDrop.DisableWebViewDrop in main.go instead.

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
)
