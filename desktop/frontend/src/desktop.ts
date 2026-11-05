// Kept separate from generated bindings so Wails can regenerate them freely.
export interface DesktopSettings {
  language: string
  systemLanguage: string
  closeToTray: boolean
  autostart: boolean
  autoConnect: boolean
  autoConnectServerId: string
  autoConnectError: string
  autoCheckUpdates: boolean
  renderingNote: string
  canInstallUpdates: boolean
  supported: boolean
  trayAvailable: boolean
  trayError: string
  installedPath: string
  vpnHelperAvailable: boolean
}

interface DesktopAPI {
  MeasurePing(serverId: string): Promise<{serverId: string, host: string, milliseconds: number}>
  SetLanguage(language: string): Promise<void>
  UpdateState(): Promise<UpdateState>
  CheckForUpdates(): Promise<void>
  DownloadUpdate(): Promise<void>
  InstallUpdate(): Promise<void>
  SetAutoCheckUpdates(enabled: boolean): Promise<void>
  DesktopSettings(): Promise<DesktopSettings>
  SetCloseToTray(enabled: boolean): Promise<void>
  SetAutostart(enabled: boolean): Promise<void>
  SetAutoConnect(enabled: boolean, serverId: string): Promise<void>
  InstallDesktop(): Promise<string>
  CreateDesktopShortcut(): Promise<string>
  InstallVPNHelper(): Promise<void>
  RemoveVPNHelper(): Promise<void>
  MinimiseToTray(): Promise<void>
  Quit(): Promise<void>
}

export interface UpdateState {
  repository: string
  currentVersion: string
  currentKnown: boolean
  latestVersion: string
  available: boolean
  assetName: string
  releaseUrl: string
  checkedAt: string
  busy: string
  phase: string
  bytesPerSecond: number
  remainingSeconds: number
  error: string
  downloadedPath: string
  received: number
  total: number
  installationMessage: string
  backupPath: string
}

export function desktop(): DesktopAPI {
  return (window as unknown as {go: {main: {App: DesktopAPI}}}).go.main.App
}
