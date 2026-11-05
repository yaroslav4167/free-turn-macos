import {useEffect, useState} from 'react'
import {desktop, DesktopSettings} from '../desktop'
import Icon from './Icon'
import UpdatePreferences from './UpdatePreferences'
import {backend} from '../../wailsjs/go/models'
import {availableLanguages, configureLanguage, tr} from '../language'

export default function DesktopPreferences({version, active, servers}: {version: string, active: boolean, servers: backend.Server[]}) {
  const [settings, setSettings] = useState<DesktopSettings | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')

  const refresh = () => desktop().DesktopSettings().then(setSettings)
  useEffect(() => {
    refresh().catch((e) => setError(String(e)))
  }, [])

  const run = async (action: () => Promise<unknown>, success = '') => {
    setBusy(true)
    setError('')
    setMessage('')
    try {
      await action()
      await refresh()
      setMessage(success)
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="settings-page" hidden={!active}>
      <header className="page-heading">
        <div>
          <span className="eyebrow">{tr('ПРИЛОЖЕНИЕ', 'APPLICATION')}</span>
          <h1>{tr('Настройки', 'Settings')}</h1>
        </div>
      </header>
      {error && (
        <div className="notice error" role="alert">
          {error}
        </div>
      )}
      {message && (
        <div className="notice info" role="status">
          {message}
        </div>
      )}
      {!settings && !error && <p>{tr('Загрузка настроек…', 'Loading settings…')}</p>}
      {settings && (
        <>
          <div className="settings-card">
            <h2>
              <Icon name="settings" /> {tr('Поведение', 'Behavior')}
            </h2>
            <div className="preference-row language-row">
              <label htmlFor="interface-language"><strong>{tr('Язык / Language', 'Language / Язык')}</strong></label>
              <select id="interface-language" aria-label={tr('Язык интерфейса', 'Interface language')}
                value={settings.language || 'system'} disabled={busy} onChange={(event) => {
                const language = event.target.value
                run(async () => {
                  await desktop().SetLanguage(language)
                  configureLanguage(language, settings.systemLanguage)
                })
              }}>
                <option value="system">{tr('Как в системе', 'System default')}</option>
                {availableLanguages().map(language => <option key={language.code} value={language.code} lang={language.code}>{language.name}</option>)}
              </select>
            </div>
            <label className="preference-row">
              <div>
                <strong>{tr('Запускать вместе с системой', 'Start at login')}</strong>
                <p>{tr('При входе в macOS. Автоподключение настраивается отдельно ниже.', 'When signing in to macOS. Automatic connection is configured separately below.')}</p>
              </div>
              <input
                type="checkbox"
                role="switch"
                checked={settings.autostart}
                disabled={busy || !settings.supported}
                onChange={(e) => run(() => desktop().SetAutostart(e.target.checked))}
              />
            </label>
            <label className="preference-row">
              <div>
                <strong>{tr('Подключаться при запуске', 'Connect on startup')}</strong>
                <p>{tr('Одна попытка при каждом запуске приложения, в том числе вместе с системой.', 'One attempt on each application launch, including at login.')}</p>
              </div>
              <input
                type="checkbox"
                role="switch"
                checked={settings.autoConnect}
                disabled={busy || (!servers.length && !settings.autoConnect)}
                onChange={(e) => run(() => desktop().SetAutoConnect(
                  e.target.checked, settings.autoConnectServerId || servers[0]?.id || '',
                ))}
              />
            </label>
            <label className="field autoconnect-profile">
              <span>{tr('Профиль автоподключения', 'Startup connection profile')}</span>
              <select
                value={settings.autoConnectServerId}
                disabled={busy || !servers.length}
                onChange={(e) => run(() => desktop().SetAutoConnect(settings.autoConnect, e.target.value))}
              >
                <option value="" disabled>{tr('Выберите профиль', 'Select a profile')}</option>
                {settings.autoConnectServerId && !servers.some((s) => s.id === settings.autoConnectServerId) && (
                  <option value={settings.autoConnectServerId} disabled>{tr('Профиль удалён — выберите другой', 'Profile deleted — select another')}</option>
                )}
                {servers.map((s) => <option key={s.id} value={s.id}>{s.name || s.id}</option>)}
              </select>
            </label>
            <p className="settings-note">
              {tr('Капча может потребовать ручного ввода. При ошибке повторите подключение вручную.', 'Captcha may require manual input. If the connection fails, try again manually.')}
            </p>
            {settings.autoConnectError && (
              <div className="notice error" role="alert">{settings.autoConnectError}</div>
            )}
            <label className="preference-row">
              <div>
                <strong>{tr('Сворачивать в трей при закрытии', 'Minimize to tray on close')}</strong>
                <p>{tr('Закрытие окна не прерывает соединение. Выход — через меню трея.', 'Closing the window keeps the connection active. Quit using the tray menu.')}</p>
              </div>
              <input
                type="checkbox"
                role="switch"
                checked={settings.closeToTray}
                disabled={busy || (!settings.trayAvailable && !settings.closeToTray)}
                onChange={(e) => run(() => desktop().SetCloseToTray(e.target.checked))}
              />
            </label>
            {!settings.trayAvailable && (
              <div className="notice info">
                {tr('Системный трей недоступен. Пока трей недоступен, закрытие окна завершает приложение и VPN.', 'System tray unavailable. Without a tray, closing the window exits the app and disconnects the VPN.')}
                {settings.trayError && (
                  <details>
                    <summary>{tr('Подробности', 'Details')}</summary>
                    <p className="mono">{settings.trayError}</p>
                  </details>
                )}
              </div>
            )}
            <button
              className="btn"
              disabled={busy || !settings.trayAvailable}
              onClick={() => run(() => desktop().MinimiseToTray())}
            >
              <Icon name="tray" size={17} /> {tr('Свернуть в трей', 'Minimize to tray')}
            </button>
          </div>
          <UpdatePreferences active={active} autoCheck={settings.autoCheckUpdates} disabled={busy} canInstall={settings.canInstallUpdates}
            onAutoCheck={(enabled) => run(() => desktop().SetAutoCheckUpdates(enabled))} />
        </>
      )}
      <footer className="settings-about">
        <img className="app-icon" src="/freeturn.svg" alt="" width="44" height="44" />
        <div>
          <strong>{tr('FreeTurn для macOS', 'FreeTurn for macOS')}</strong>
          <p>Core {version || '…'} · Wails + React</p>
        </div>
        <button className="btn danger" onClick={() => run(() => desktop().Quit())}>
          {tr('Выйти из приложения', 'Quit application')}
        </button>
      </footer>
    </section>
  )
}
