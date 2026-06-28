import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { LogtoProvider } from '@logto/react'
import { logtoConfig } from './logto'
import App from './App.jsx'
import './styles.css'

ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <LogtoProvider config={logtoConfig}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </LogtoProvider>
  </React.StrictMode>,
)
