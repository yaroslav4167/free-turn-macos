import {backend} from '../api'
import {tr} from '../language'
import InfoTip from './InfoTip'

type RouteList = backend.Server['routingLists'][number]

export default function VPNRoutingLists({server, disabled, onChange}: {
  server: backend.Server
  disabled: boolean
  onChange: (server: backend.Server) => void
}) {
  const lists = server.routingLists ?? []
  const setLists = (next: RouteList[]) => onChange({...server, routingLists: next} as backend.Server)
  const patch = (index: number, change: Partial<RouteList>) => setLists(lists.map((list, i) => i === index ? {...list, ...change} as RouteList : list))
  return (
    <section className="field-group vpn-routing-lists">
      <div className="routing-list-heading">
        <h3>{tr('Списки маршрутизации VPN', 'VPN routing lists')}</h3>
        <InfoTip label={tr('О режимах списков маршрутизации')}>
        {tr('Белый список: только указанные адреса через VPN. Чёрный: указанные адреса в обход VPN. Белые списки объединяются, чёрные исключения имеют приоритет над белыми. Без включённых белых списков используется весь диапазон AllowedIPs профиля. Правила действуют только во встроенном VPN, не в режиме реле или отдельном CLI-клиенте.', 'Whitelist: only listed addresses use VPN. Blacklist: listed addresses bypass VPN. Whitelists are combined; blacklist exclusions take priority. With no enabled whitelist, the full profile AllowedIPs range is used. Rules apply only to the built-in VPN, not relay mode or the separate CLI client.')}
        </InfoTip>
      </div>
      {lists.map((list, index) => (
        <fieldset className="routing-list" key={index} disabled={disabled}>
          <legend>{tr('Список', 'List')} {index + 1}</legend>
          <div className="field-row">
            <div className="field">
              <label htmlFor={`routing-name-${index}`}>{tr('Название', 'Name')}</label>
              <input id={`routing-name-${index}`} type="text" maxLength={80} value={list.name}
                onChange={(e) => patch(index, {name: e.target.value})} />
            </div>
            <div className="field">
              <label htmlFor={`routing-mode-${index}`}>{tr('Режим списка', 'List mode')}</label>
              <select id={`routing-mode-${index}`} value={list.mode}
                onChange={(e) => patch(index, {mode: e.target.value})}>
                <option value="blacklist">{tr('Чёрный — в обход VPN', 'Blacklist — bypass VPN')}</option>
                <option value="whitelist">{tr('Белый — через VPN', 'Whitelist — use VPN')}</option>
              </select>
            </div>
          </div>
          <div className="field">
            <label htmlFor={`routing-entries-${index}`}>{tr('Домены, IP и подсети — по одной записи в строке', 'Domains, IPs and subnets — one entry per line')}</label>
            <textarea id={`routing-entries-${index}`} rows={6} spellCheck={false}
              value={(list.entries ?? []).join('\n')}
              placeholder={'example.com\n203.0.113.5\n198.51.100.0/24\n2001:db8::/32'}
              onChange={(e) => patch(index, {entries: e.target.value.split('\n')})} />
          </div>
          <div className="routing-list-actions">
            <label className="checkbox-row">
              <input type="checkbox" checked={list.enabled} onChange={(e) => patch(index, {enabled: e.target.checked})} />
              {tr('Использовать список', 'Enable list')}
            </label>
            <button className="btn small danger" onClick={() => setLists(lists.filter((_, i) => i !== index))}>{tr('Удалить список', 'Delete list')}</button>
          </div>
        </fieldset>
      ))}
      <button className="btn small" disabled={disabled || lists.length >= 16}
        onClick={() => setLists([...lists, {name: tr('Список {number}', {number: lists.length + 1}), mode: 'blacklist', enabled: true, entries: []} as RouteList])}>
        {tr('Добавить список', 'Add list')}
      </button>
      <p className="settings-note">
        {tr('До 16 списков и 256 записей на профиль. Сохраните профиль и переподключитесь для применения.', 'Up to 16 lists and 256 entries per profile. Save and reconnect to apply.')}
        {disabled && tr(' Изменение списков недоступно, пока этот профиль подключён.', ' Lists cannot be edited while this profile is connected.')}
        {' '}
        <InfoTip label={tr('Об ограничениях маршрутизации')}>
          {tr('Домены разрешаются через системный DNS при подключении; изменение их IP требует переподключения. Указывайте точные имена, без https://, путей и *. Поддомены добавляются отдельно. Сайты на одном IP могут попадать под одно правило; собственный DNS/DoH браузера может дать другие IP. Фильтруются только диапазоны AllowedIPs конфигурации WireGuard: например, при наличии только IPv4 правила не охватывают IPv6. DNS-серверы профиля остаются служебными VPN-маршрутами даже при чёрном исключении. На macOS SSH следует маршрутам профиля: для доступа через VPN включите адрес VPS в VPN-маршруты. Соединения самого реле идут через физический интерфейс. Это маршрутизация, не блокировщик и не защита от утечек.')}
        </InfoTip>
      </p>
    </section>
  )
}
