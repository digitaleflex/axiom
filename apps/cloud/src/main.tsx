import React from 'react'
import { createRoot } from 'react-dom/client'
import './styles.css'

function App() {
  return (
    <main className="shell">
      <header className="topbar">
        <div className="brand"><span className="mark">A</span><span>Axiom</span></div>
        <div className="status"><span className="dot" /> Engine online</div>
      </header>

      <section className="hero">
        <div>
          <p className="eyebrow">CLOUD / V0.1</p>
          <h1>From GitHub<br />to production.</h1>
          <p className="subtitle">Connect a repository, let Axiom understand its stack, then deploy it to your infrastructure.</p>
          <button className="primary">+ New Project</button>
        </div>
        <div className="pipeline">
          {['GitHub', 'Analyze', 'Detect', 'Build', 'Deploy', 'Live'].map((step, i) => (
            <div className="step" key={step}>
              <span>{String(i + 1).padStart(2, '0')}</span>
              <strong>{step}</strong>
              {i < 5 && <i />}
            </div>
          ))}
        </div>
      </section>

      <section className="stats">
        <div><span>PROJECTS</span><strong>0</strong></div>
        <div><span>DEPLOYMENTS</span><strong>0</strong></div>
        <div><span>SERVERS</span><strong>0</strong></div>
        <div><span>HEALTH</span><strong className="muted">—</strong></div>
      </section>

      <section className="empty">
        <div className="terminal">$ axiom deploy</div>
        <h2>No applications yet</h2>
        <p>Your first deployment will appear here.</p>
        <button className="secondary">Create your first project</button>
      </section>
    </main>
  )
}

createRoot(document.getElementById('root')!).render(<React.StrictMode><App /></React.StrictMode>)
