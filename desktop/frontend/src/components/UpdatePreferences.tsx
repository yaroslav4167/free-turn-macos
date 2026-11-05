import {useEffect, useState} from 'react'
import {desktop, UpdateState} from '../desktop'
import Icon from './Icon'
import InfoTip from './InfoTip'
import {currentLanguage, tr} from '../language'
import {readDismissedUpdate, saveDismissedUpdate, updateNoticeId} from '../updateNotice'
import {updateProgress, UpdateAction} from '../updateProgress'

function useUpdateState(interval: number, enabled = true) {
  const [state, setState] = useState<UpdateState | null>(null)
  useEffect(() => {
    if (!enabled) return
    let active = true
    let timer: number | undefined
    const refresh = async () => {
      try {
        const next = await desktop().UpdateState()
        if (active) setState(next)
      } catch {
        // The local bridge can disappear while the application restarts.
      } finally {
        if (active) timer = window.setTimeout(refresh, interval)
      }
    }
    refresh()
    return () => {
      active = false
      window.clearTimeout(timer)
    }
  }, [interval, enabled])
  return state
}

export function UpdateNotice({onOpen}: {onOpen: () => void}) {
  const state = useUpdateState(10000)
  const [dismissed, setDismissed] = useState(() => readDismissedUpdate())
  if (!state?.available || dismissed === updateNoticeId(state)) return null
  const dismiss = () => {
    const id = updateNoticeId(state)
    setDismissed(id)
    saveDismissedUpdate(id)
  }
  return (
    <div className="notice info app-error" role="status">
      <span>{state.currentKnown ? tr('Доступно обновление', 'Update available') : tr('Доступен стабильный релиз', 'Stable release available')} FreeTurn {state.latestVersion}.</span>
      <div className="inline-actions update-notice-actions">
        <button className="btn small" onClick={onOpen}>
          {tr('Открыть настройки', 'Open settings')}
        </button>
        <button className="btn small ghost" onClick={dismiss}
          aria-label={tr('Скрыть уведомление об обновлении', 'Dismiss update notification')} title={tr('Скрыть уведомление об этой версии', 'Dismiss notification for this version')}>
          ×
        </button>
      </div>
    </div>
  )
}

export default function UpdatePreferences({active, autoCheck, onAutoCheck, disabled, canInstall}: {
  active: boolean
  autoCheck: boolean
  onAutoCheck: (enabled: boolean) => void
  disabled: boolean
  canInstall: boolean
}) {
  const state = useUpdateState(500, active)
  const [pending, setPending] = useState<UpdateAction | null>(null)
  const [error, setError] = useState('')
  const run = async (operation: UpdateAction, action: () => Promise<void>) => {
    setPending(operation)
    setError('')
    try {
      await action()
    } catch (e) {
      setError(String(e))
    } finally {
      setPending(null)
    }
  }
  const busy = pending !== null || !!state?.busy
  const progress = updateProgress(state, pending)
  return (
    <div className="settings-card updates-card">
      <h2>
        <Icon name="download" /> {tr('Обновления', 'Updates')}
      </h2>
      <div className="update-summary">
        <span>{tr('Версия:', 'Version:')} <strong>{state?.currentVersion || '…'}</strong></span>
        <InfoTip label={tr('Об источнике обновлений', 'About the update source')}>
          {state?.repository ? tr('Источник: github.com/{repository}. Только стабильные релизы; ключи и профили не передаются.', {repository: state.repository})
            : tr('Репозиторий обновлений не задан при сборке. Для локальной сборки задайте UPDATE_REPOSITORY.', 'No update repository configured in this build. Set UPDATE_REPOSITORY for local builds.')}
          {state && !state.currentKnown && <p>{tr('Тестовая сборка: сравнение версий недоступно.', 'Development build: version comparison unavailable.')}</p>}
          {state?.checkedAt && <p>{tr('Проверено:', 'Last checked:')} {new Date(state.checkedAt).toLocaleString(currentLanguage())}</p>}
        </InfoTip>
        {state?.checkedAt && <span>{state.available ? tr('Доступна {version}', {version: state.latestVersion}) : tr('Обновлений нет', 'No updates')}</span>}
      </div>
      <div className="preference-row">
        <div className="update-summary">
          <label htmlFor="auto-check-updates"><strong>{tr('Автопроверка', 'Automatic checks')}</strong></label>
          <InfoTip label={tr('О расписании автопроверки', 'About the update schedule')}>{tr('Через 10 секунд после запуска, затем каждые 6 часов через публичный GitHub API.', '10 seconds after launch, then every 6 hours using the public GitHub API.')}</InfoTip>
        </div>
        <input
          id="auto-check-updates"
          type="checkbox"
          role="switch"
          checked={autoCheck}
          disabled={disabled || !state?.repository}
          onChange={(e) => onAutoCheck(e.target.checked)}
        />
      </div>
      {(error || (!busy && state?.error)) && (
        <div className="notice error" role="alert">{error || state?.error}</div>
      )}
      <div className="inline-actions update-actions">
        <button className="btn" disabled={busy || !state?.repository} onClick={() => run('checking', () => desktop().CheckForUpdates())}>
          {state?.busy === 'checking' ? tr('Проверка…', 'Checking…') : tr('Проверить обновления', 'Check for updates')}
        </button>
        {state?.available && canInstall && (
          <button className="btn primary" disabled={busy} onClick={() => run('installing', () => desktop().InstallUpdate())}>
            {pending === 'installing' || state.busy === 'preparing' || state.busy === 'restarting' ? tr('Подготовка обновления…', 'Preparing update…') : tr('Скачать и установить', 'Download and install')}
          </button>
        )}
        {state?.available && (
          <button className="btn primary" disabled={busy} onClick={() => run('downloading', () => desktop().DownloadUpdate())}>
            <Icon name="download" size={17} /> {state.busy === 'downloading' ? tr('Скачивание…', 'Downloading…') : tr('Скачать {version}', {version: state.latestVersion})}
          </button>
        )}
      </div>
      {progress && (
        <section className="update-task" aria-label={tr('Ход обновления', 'Update progress')} aria-busy="true">
          <div className="update-task-heading">
            <strong role="status">{progress.title}</strong>
            {progress.percent !== null && <span className="mono">{progress.percent}%</span>}
          </div>
          <progress className="update-progress" aria-label={progress.title} max={100} value={progress.percent ?? undefined} />
          {progress.transfer && (
            <div className="update-task-stats">
              <span>{progress.transfer}</span>
              <span>{progress.speed}</span>
              <span>{progress.eta}</span>
            </div>
          )}
          <p className="settings-note">{progress.detail}</p>
        </section>
      )}
      {!canInstall && <p className="settings-note">{tr('После скачивания закройте FreeTurn и замените приложение в папке «Программы».', 'After downloading, quit FreeTurn and replace the app in Applications.')}</p>}
      {state?.downloadedPath && !busy && (
        <div className="notice info" role="status">
          {tr('Архив проверен по SHA-256 и сохранён:', 'Archive verified with SHA-256 and saved:')} <span className="mono update-path">{state.downloadedPath}</span>
        </div>
      )}
    </div>
  )
}
