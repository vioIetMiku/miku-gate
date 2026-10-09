import { NetworkPlanView } from './NetworkPlanView';
import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';
import {
  ArrowClockwise,
  ArrowRight,
  ArrowsLeftRight,
  CaretRight,
  Check,
  CheckCircle,
  CircleNotch,
  Clock,
  Copy,
  Database,
  DotsThree,
  GlobeHemisphereWest,
  Info,
  Key,
  List,
  MagnifyingGlass,
  Moon,
  Pause,
  Plus,
  Power,
  ShieldCheck,
  ShieldWarning,
  SignOut,
  SlidersHorizontal,
  Sun,
  TerminalWindow,
  Trash,
  Warning,
  WaveSine,
  X,
} from '@phosphor-icons/react';
import {
  api,
  countries,
  filterExits,
  policies,
  setCSRF,
  timestamp,
  type Diagnostics,
  type Event as AuditEvent,
  type Exit,
  type ExitInput,
  type Node as VPNNode,
  type Overview,
  type Session,
} from './api';

type Page = 'overview' | 'exits' | 'nodes' | 'events' | 'system';
const navigation: { id: Page; title: string; icon: typeof GlobeHemisphereWest }[] = [
  { id: 'overview', title: '概览', icon: GlobeHemisphereWest },
  { id: 'exits', title: '出口工作台', icon: ArrowsLeftRight },
  { id: 'nodes', title: '节点库', icon: Database },
  { id: 'events', title: '事件日志', icon: Clock },
  { id: 'system', title: '系统与诊断', icon: SlidersHorizontal },
];
function Logo({ large = false }: { large?: boolean }) {
  return (
    <div className={`brand ${large ? 'brand-large' : ''}`}>
      <span className="brand-mark">
        <WaveSine size={large ? 30 : 24} weight="bold" />
      </span>
      <span>
        Miku <b>Gate</b>
        <small>YOUR CONNECTION, YOUR WAY</small>
      </span>
    </div>
  );
}
function Spinner() {
  return <CircleNotch className="spin" size={17} />;
}
function Pill({ children, tone = '' }: { children: ReactNode; tone?: string }) {
  return <span className={`pill ${tone}`}>{children}</span>;
}
function Modal({
  title,
  children,
  close,
  wide = false,
}: {
  title: string;
  children: ReactNode;
  close: () => void;
  wide?: boolean;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    dialog.current?.showModal();
    const el = dialog.current;
    return () => el?.close();
  }, []);
  return (
    <dialog
      ref={dialog}
      className={wide ? 'wide' : ''}
      onCancel={(event) => {
        event.preventDefault();
        close();
      }}
      aria-label={title}
    >
      <div className="modal-heading">
        <h2>{title}</h2>
        <button className="icon-button" onClick={close} aria-label="关闭对话框">
          <X size={20} />
        </button>
      </div>
      {children}
    </dialog>
  );
}
function Login({ onLogin }: { onLogin: (s: Session) => void }) {
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      onLogin(await api<Session>('/auth/login', 'POST', { username, password }));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="login-page">
      <aside className="login-aside">
        <Logo large />
        <div className="login-intro">
          <span className="eyebrow">A LITTLE MORE CONTROL.</span>
          <h1>
            每一条连接，
            <br />
            都有清楚的方向。
          </h1>
          <p>
            把出口、节点和策略放在一起。
            <br />
            从你的第一条连接开始，有条理地管理网络。
          </p>
          <div className="login-lines">
            <div>
              <ArrowsLeftRight size={21} />
              <span>独立出口配置</span>
            </div>
            <div>
              <ShieldCheck size={21} />
              <span>受保护的本机控制台</span>
            </div>
            <div>
              <TerminalWindow size={21} />
              <span>可追溯的操作记录</span>
            </div>
          </div>
        </div>
        <footer>
          MIKU GATE <span>LOCAL-FIRST · OPEN DESIGN</span>
        </footer>
      </aside>
      <main className="login-main">
        <div className="login-box">
          <Pill tone="teal">CONTROL PLANE / DEV</Pill>
          <h2>欢迎回来</h2>
          <p className="muted">登录你的 Miku Gate 控制台。</p>
          <form onSubmit={submit}>
            <label>
              用户名
              <input
                autoComplete="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                required
                maxLength={40}
                autoFocus
              />
            </label>
            <label>
              密码
              <input
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                maxLength={1024}
              />
            </label>
            {error && (
              <div className="inline-error" role="alert">
                {error}
              </div>
            )}
            <button className="button primary full" disabled={busy}>
              {busy ? <Spinner /> : '登录控制台'}
              {!busy && <ArrowRight size={18} />}
            </button>
          </form>
          <div className="init-hint">
            <TerminalWindow size={19} />
            <div>
              <strong>首次使用？在本机初始化管理员</strong>
              <code>miku-gate init</code>
              <p>随机密码仅在初始化时显示。没有默认公共密码。</p>
            </div>
          </div>
          <p className="login-note">
            <Info size={16} />
            当前版本只管理配置，尚未接入 VPN 网络执行器。
          </p>
        </div>
      </main>
    </div>
  );
}
function ExitForm({
  current,
  submit,
  close,
}: {
  current?: Exit;
  submit: (input: ExitInput) => Promise<void>;
  close: () => void;
}) {
  const [name, setName] = useState(current?.name ?? '');
  const [country, setCountry] = useState(current?.country ?? 'JP');
  const [policy, setPolicy] = useState(current?.policy ?? 'any');
  const [port, setPort] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function save(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await submit({
        name,
        country,
        policy,
        ...(!current ? { port: port ? Number(port) : 0 } : {}),
      });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title={current ? '编辑出口' : '创建一个新出口'}
      close={() => {
        if (!busy) close();
      }}
    >
      <form onSubmit={save} className="modal-form">
        <p className="muted">为一组代理连接定义地区和节点策略。</p>
        <label>
          出口名称
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="例如：日本开发环境"
            maxLength={48}
            required
            autoFocus
          />
        </label>
        <div className="form-grid">
          <label>
            出口地区
            <select value={country} onChange={(e) => setCountry(e.target.value)}>
              {Object.entries(countries).map(([code, title]) => (
                <option key={code} value={code}>
                  {title} · {code}
                </option>
              ))}
            </select>
          </label>
          <label>
            代理端口
            {current ? (
              <input value={current.port} disabled />
            ) : (
              <input
                inputMode="numeric"
                type="number"
                min={1024}
                max={65535}
                value={port}
                onChange={(e) => setPort(e.target.value)}
                placeholder="自动分配"
              />
            )}
          </label>
        </div>
        <label>
          节点类型策略
          <select value={policy} onChange={(e) => setPolicy(e.target.value)}>
            {Object.entries(policies).map(([v, text]) => (
              <option value={v} key={v}>
                {text}
              </option>
            ))}
          </select>
        </label>
        <div className="field-note">
          类型情报尚未接入。未知类型不会被视为“已验证住宅”，策略先保存为配置。
        </div>
        <div className="notice">
          <ShieldWarning size={21} />
          <p>当前仅保存配置。网络执行器接入并验证前，代理不会监听，也不会建立 VPN 隧道。</p>
        </div>
        {error && (
          <div className="inline-error" role="alert">
            {error}
          </div>
        )}
        <div className="modal-actions">
          <button type="button" className="button" onClick={close} disabled={busy}>
            取消
          </button>
          <button className="button primary" disabled={busy}>
            {busy ? <Spinner /> : <Plus size={17} />}
            {current ? '保存更改' : '创建出口'}
          </button>
        </div>
      </form>
    </Modal>
  );
}
function Secret({
  exit,
  password,
  close,
  notify,
}: {
  exit: Exit;
  password: string;
  close: () => void;
  notify: (m: string) => void;
}) {
  const value = `socks5://${exit.username}:${password}@${exit.listen}:${exit.port}`;
  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      notify('已复制，请妥善保存');
    } catch {
      notify('剪贴板不可用，请手动选择复制');
    }
  }
  return (
    <Modal title="保存你的代理凭据" close={close}>
      <div className="modal-form">
        <div className="notice">
          <Key size={22} />
          <p>密码只显示这一次，关闭后无法查看；可在出口详情中重新生成。</p>
        </div>
        <dl className="credential-list">
          <div>
            <dt>用户名</dt>
            <dd>
              <code>{exit.username}</code>
            </dd>
          </div>
          <div>
            <dt>密码</dt>
            <dd>
              <code className="secret-value">{password}</code>
              <button className="icon-button" onClick={() => copy(password)} aria-label="复制密码">
                <Copy size={17} />
              </button>
            </dd>
          </div>
          <div>
            <dt>入口</dt>
            <dd>
              <code>
                {exit.listen}:{exit.port}
              </code>
            </dd>
          </div>
        </dl>
        <div className="inline-warning">
          这是已预留的配置地址，不是已启动的代理。请勿把未启用入口当作可用节点。
        </div>
        <div className="modal-actions">
          <button className="button" onClick={() => copy(value)}>
            <Copy size={17} />
            复制 SOCKS5 配置
          </button>
          <button className="button primary" onClick={close}>
            已保存
          </button>
        </div>
      </div>
    </Modal>
  );
}
function Empty({
  icon: Icon,
  title,
  detail,
  action,
}: {
  icon: typeof Database;
  title: string;
  detail: string;
  action?: ReactNode;
}) {
  return (
    <div className="empty">
      <span className="empty-icon">
        <Icon size={30} weight="light" />
      </span>
      <h3>{title}</h3>
      <p>{detail}</p>
      {action}
    </div>
  );
}

export default function App() {
  const sessionEpoch = useRef(0);
  const [session, setSession] = useState<Session | null>(null);
  const [booting, setBooting] = useState(true);
  const [page, setPage] = useState<Page>('overview');
  const [mobileNav, setMobileNav] = useState(false);
  const [theme, setTheme] = useState(() => {
    try {
      return localStorage.getItem('miku-theme') ?? 'light';
    } catch {
      return 'light';
    }
  });
  const [overview, setOverview] = useState<Overview | null>(null);
  const [exits, setExits] = useState<Exit[]>([]);
  const [nodes, setNodes] = useState<VPNNode[]>([]);
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [diagnostics, setDiagnostics] = useState<Diagnostics | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [toast, setToast] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [actionBusy, setActionBusy] = useState(false);
  const [form, setForm] = useState<Exit | 'new' | null>(null);
  const [detail, setDetail] = useState<Exit | null>(null);
  const [deleting, setDeleting] = useState<Exit | null>(null);
  const [secret, setSecret] = useState<{ exit: Exit; password: string } | null>(null);
  const [query, setQuery] = useState('');
  const [country, setCountry] = useState('');
  const [nodeQuery, setNodeQuery] = useState('');
  const [nodeLimit, setNodeLimit] = useState(50);
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem('miku-theme', theme);
    } catch {
      /* storage is optional */
    }
  }, [theme]);
  const acceptSession = useCallback((s: Session | null) => {
    sessionEpoch.current += 1;
    setLoading(false);
    setCSRF(s?.csrf ?? '');
    setSession(s);
    if (!s) {
      setOverview(null);
      setExits([]);
      setNodes([]);
      setEvents([]);
      setForm(null);
      setDetail(null);
      setSecret(null);
      setDeleting(null);
    }
  }, []);
  useEffect(() => {
    let alive = true;
    api<Session>('/auth/session')
      .then((s) => {
        if (alive) acceptSession(s);
      })
      .catch(() => {})
      .finally(() => {
        if (alive) setBooting(false);
      });
    const expire = () => acceptSession(null);
    window.addEventListener('miku:session-expired', expire);
    return () => {
      alive = false;
      window.removeEventListener('miku:session-expired', expire);
    };
  }, [acceptSession]);
  useEffect(() => {
    if (!toast) return;
    const id = window.setTimeout(() => setToast(''), 6500);
    return () => clearTimeout(id);
  }, [toast]);
  const load = useCallback(async () => {
    if (!session) return;
    const epoch = sessionEpoch.current;
    setLoading(true);
    try {
      const [o, e, n, v, d] = await Promise.all([
        api<Overview>('/overview'),
        api<Exit[]>('/exits'),
        api<VPNNode[]>('/nodes'),
        api<AuditEvent[]>('/events'),
        api<Diagnostics>('/diagnostics'),
      ]);
      if (epoch !== sessionEpoch.current) return;
      setOverview(o);
      setExits(e);
      setNodes(n);
      setEvents(v);
      setDiagnostics(d);
      setLoadError('');
    } catch (e) {
      if (epoch === sessionEpoch.current) setLoadError((e as Error).message);
    } finally {
      if (epoch === sessionEpoch.current) setLoading(false);
    }
  }, [session]);
  useEffect(() => {
    void load();
    if (!session) return;
    const id = window.setInterval(() => {
      if (!document.hidden) void load();
    }, 20000);
    return () => clearInterval(id);
  }, [load, session]);
  async function mutate(fn: () => Promise<unknown>, success: string) {
    if (actionBusy) return;
    setActionBusy(true);
    try {
      await fn();
      setToast(success);
    } catch (e) {
      setToast((e as Error).message);
    } finally {
      await load();
      setActionBusy(false);
    }
  }
  async function logout() {
    if (actionBusy) return;
    setActionBusy(true);
    try {
      await api('/auth/logout', 'POST');
      acceptSession(null);
      setToast('');
    } catch (e) {
      setToast((e as Error).message);
    } finally {
      setActionBusy(false);
    }
  }
  async function saveExit(input: ExitInput) {
    if (form && form !== 'new') {
      await api(`/exits/${form.id}`, 'PATCH', input);
      setForm(null);
      setToast('出口配置已更新');
    } else {
      const result = await api<{ exit: Exit; password: string }>('/exits', 'POST', input);
      setForm(null);
      setSecret(result);
    }
    await load();
  }
  async function refreshNodes() {
    setRefreshing(true);
    try {
      const result = await api<{ count: number; skipped: number }>('/nodes/refresh', 'POST');
      setToast(
        `已同步 ${result.count} 条候选，跳过 ${result.skipped} 条无效配置。尚未测试连通性。`,
      );
      await load();
    } catch (e) {
      setToast((e as Error).message);
    } finally {
      setRefreshing(false);
    }
  }
  async function rotate(exit: Exit) {
    await mutate(async () => {
      const result = await api<{ password: string }>(`/exits/${exit.id}/credentials`, 'POST');
      setDetail(null);
      setSecret({ exit, password: result.password });
    }, '代理凭据已重新生成');
  }
  const filtered = filterExits(exits, query, country);
  const filteredNodes = nodes.filter((n) =>
    `${n.hostname} ${n.ip} ${n.country} ${n.country_name}`
      .toLowerCase()
      .includes(nodeQuery.toLowerCase()),
  );
  if (booting)
    return (
      <div className="boot">
        <Logo />
        <Spinner />
        <p>连接本机控制台…</p>
      </div>
    );
  if (!session) return <Login onLogin={acceptSession} />;
  return (
    <div className="app-shell">
      <aside className={`sidebar ${mobileNav ? 'open' : ''}`}>
        <Logo />
        <div className="workspace">
          <span className="workspace-symbol">
            <TerminalWindow size={19} />
          </span>
          <div>
            <strong>本机工作空间</strong>
            <span>LOCAL WORKSPACE</span>
          </div>
          <Pill>DEV</Pill>
        </div>
        <span className="nav-caption">工作空间</span>
        <nav>
          {navigation.map(({ id, title, icon: Icon }) => (
            <button
              key={id}
              className={`nav-item ${page === id ? 'active' : ''}`}
              onClick={() => {
                setPage(id);
                setMobileNav(false);
              }}
            >
              <Icon size={21} weight={page === id ? 'duotone' : 'regular'} />
              <span>{title}</span>
              {id === 'exits' && exits.length > 0 && <small>{exits.length}</small>}
            </button>
          ))}
        </nav>
        <div className="coming">
          <span className="nav-caption">下一步</span>
          <div>
            <Key size={18} />
            客户端与订阅<span>v0.2</span>
          </div>
          <div>
            <GlobeHemisphereWest size={18} />
            多服务器管理<span>v0.3</span>
          </div>
        </div>
        <div className="sidebar-bottom">
          <div className="build-note">
            <span className="status-dot amber" />
            <div>
              <strong>控制台开发版</strong>
              <p>VPN 数据面尚未接入</p>
            </div>
          </div>
          <button className="account" onClick={() => void logout()} disabled={actionBusy}>
            <span className="avatar">{session.username[0].toUpperCase()}</span>
            <span>
              <strong>{session.username}</strong>
              <small>本机管理员</small>
            </span>
            <SignOut size={19} />
          </button>
        </div>
      </aside>
      {mobileNav && (
        <button className="nav-scrim" onClick={() => setMobileNav(false)} aria-label="关闭导航" />
      )}
      <div className="app-body">
        <header className="topbar">
          <div className="breadcrumbs">
            <button
              className="icon-button menu-button"
              aria-label="打开导航"
              onClick={() => setMobileNav(true)}
            >
              <List size={21} />
            </button>
            <span>工作空间</span>
            <CaretRight size={13} />
            <strong>{navigation.find((n) => n.id === page)?.title}</strong>
          </div>
          <div className="top-actions">
            <span className="local-label">
              <span className="status-dot" />
              本机控制台
            </span>
            <button
              className="icon-button"
              onClick={() => setTheme(theme === 'light' ? 'dark' : 'light')}
              aria-label={theme === 'light' ? '切换深色主题' : '切换浅色主题'}
            >
              {theme === 'light' ? <Moon size={19} /> : <Sun size={19} />}
            </button>
            <span className="version">{overview?.version ?? 'DEV'}</span>
          </div>
        </header>
        <main className="content">
          <div className="page-heading">
            <div>
              <div className="eyebrow">MIKU GATE / {page.toUpperCase()}</div>
              <h1>
                {page === 'overview'
                  ? '连接的全貌，尽在这里。'
                  : navigation.find((n) => n.id === page)?.title}
              </h1>
              <p>
                {page === 'overview'
                  ? '管理你的出口，了解每一次连接。'
                  : page === 'exits'
                    ? '固定入口，独立策略。每个出口都有自己的方向。'
                    : page === 'nodes'
                      ? '发现候选，不把列表数据误认为实际测试结果。'
                      : page === 'events'
                        ? '每一次配置变更，都有迹可循。'
                        : '真实环境、能力边界与部署状态。'}
              </p>
            </div>
            <div className="heading-actions">
              <button
                className="button"
                disabled={loading}
                onClick={() => void load()}
                aria-label="刷新状态"
              >
                {loading ? <Spinner /> : <ArrowClockwise size={17} />}
                <span className="hide-small">刷新</span>
              </button>
              {(page === 'overview' || page === 'exits') && (
                <button className="button primary" onClick={() => setForm('new')}>
                  <Plus size={18} />
                  新建出口
                </button>
              )}
              {page === 'nodes' && (
                <button
                  className="button primary"
                  disabled={refreshing}
                  onClick={() => void refreshNodes()}
                >
                  {refreshing ? <Spinner /> : <ArrowClockwise size={18} />}
                  {refreshing ? '同步中…' : '同步官方节点'}
                </button>
              )}
            </div>
          </div>
          {loadError && (
            <div className="error-banner" role="alert">
              <Warning size={19} />
              {loadError}
              <button className="text-button" onClick={() => void load()}>
                重试
              </button>
            </div>
          )}
          <div className="capability-banner">
            <span className="banner-symbol">
              <ShieldWarning size={21} />
            </span>
            <div>
              <strong>先保存配置，再连接世界。</strong>
              <span>
                当前为控制台开发版：没有活动 VPN 隧道或代理监听，断线保护尚未经 Linux 验证。
              </span>
            </div>
            <button className="text-button" onClick={() => setPage('system')}>
              查看能力边界
              <ArrowRight size={15} />
            </button>
          </div>
          {(page === 'overview' || page === 'exits') && (
            <>
              {page === 'overview' && (
                <>
                  <section className="metrics" aria-label="工作空间统计">
                    {[
                      {
                        label: '出口配置',
                        value: overview?.exit_count,
                        note: '已保存的逻辑出口',
                        icon: ArrowsLeftRight,
                      },
                      {
                        label: '活动隧道',
                        value: overview?.active_count,
                        note: 'VPN 数据面未接入',
                        icon: WaveSine,
                      },
                      {
                        label: '覆盖地区',
                        value: overview?.country_count,
                        note: '根据出口配置统计',
                        icon: GlobeHemisphereWest,
                      },
                      {
                        label: '候选节点',
                        value: overview?.node_count,
                        note: '尚未验证实际可用性',
                        icon: Database,
                      },
                    ].map(({ label, value, note, icon: Icon }) => (
                      <article className="metric" key={label}>
                        <div>
                          <span>{label}</span>
                          <Icon size={20} />
                        </div>
                        <strong>
                          {value === undefined ? '—' : String(value).padStart(2, '0')}
                        </strong>
                        <p>{note}</p>
                      </article>
                    ))}
                  </section>
                  <section className="route-panel">
                    <div className="route-intro">
                      <span className="eyebrow">NETWORK PATH</span>
                      <h3>入口保持不变，出口独立管理。</h3>
                      <p>实际网络路径将在执行器接入并通过验证后显示。</p>
                    </div>
                    <div
                      className="route-diagram"
                      aria-label="预期路径：客户端、Miku Gate、待连接的 VPN 出口"
                    >
                      <div className="route-node">
                        <span>
                          <TerminalWindow size={22} />
                        </span>
                        <strong>客户端</strong>
                        <small>HTTP / SOCKS5</small>
                      </div>
                      <div className="route-line" />
                      <div className="route-node central">
                        <span>
                          <WaveSine size={23} />
                        </span>
                        <strong>Miku Gate</strong>
                        <small>稳定入口配置</small>
                      </div>
                      <div className="route-line dashed" />
                      <div className="route-node pending">
                        <span>
                          <GlobeHemisphereWest size={24} />
                        </span>
                        <strong>VPN 出口</strong>
                        <small>未连接</small>
                      </div>
                    </div>
                  </section>
                </>
              )}
              <section className="panel">
                <div className="section-heading">
                  <div>
                    <h2>
                      我的出口 <span className="count">{exits.length}</span>
                    </h2>
                    <p>这里显示配置状态，而非已建立的 VPN 连接。</p>
                  </div>
                  <Pill>
                    <span className="status-dot gray" />
                    仅配置
                  </Pill>
                </div>
                <div className="table-toolbar">
                  <div className="search">
                    <MagnifyingGlass size={18} />
                    <input
                      aria-label="搜索出口"
                      placeholder="搜索名称、地区或端口…"
                      value={query}
                      onChange={(e) => setQuery(e.target.value)}
                    />
                  </div>
                  <select
                    aria-label="筛选出口地区"
                    value={country}
                    onChange={(e) => setCountry(e.target.value)}
                  >
                    <option value="">全部地区</option>
                    {[...new Set(exits.map((e) => e.country))].sort().map((c) => (
                      <option key={c} value={c}>
                        {countries[c] ?? c}
                      </option>
                    ))}
                  </select>
                </div>
                {!filtered.length ? (
                  <Empty
                    icon={ArrowsLeftRight}
                    title={exits.length ? '没有匹配的出口' : '从第一个出口开始'}
                    detail={
                      exits.length
                        ? '试试其他名称或地区。'
                        : '选择地区和节点策略，创建一个独立的代理入口配置。'
                    }
                    action={
                      !exits.length && (
                        <button className="button primary" onClick={() => setForm('new')}>
                          <Plus size={17} />
                          创建出口
                        </button>
                      )
                    }
                  />
                ) : (
                  <div className="table-scroll">
                    <table>
                      <thead>
                        <tr>
                          <th>出口 / 地区</th>
                          <th>代理入口</th>
                          <th>节点策略</th>
                          <th>状态</th>
                          <th className="align-right">操作</th>
                        </tr>
                      </thead>
                      <tbody>
                        {filtered.map((e) => (
                          <tr key={e.id}>
                            <td>
                              <button className="exit-name" onClick={() => setDetail(e)}>
                                <span className={`country-code country-${e.country.toLowerCase()}`}>
                                  {e.country}
                                </span>
                                <span>
                                  <strong>{e.name}</strong>
                                  <small>{countries[e.country] ?? e.country}</small>
                                </span>
                              </button>
                            </td>
                            <td>
                              <code>
                                {e.listen}:{e.port}
                              </code>
                              <small className="subline">HTTP / SOCKS5 · 未监听</small>
                            </td>
                            <td>
                              <span className="policy-name">{policies[e.policy]}</span>
                            </td>
                            <td>
                              <Pill tone={e.status === 'blocked' ? 'amber' : ''}>
                                {e.status === 'blocked' ? (
                                  <ShieldWarning size={13} />
                                ) : (
                                  <Pause size={12} />
                                )}
                                {e.status === 'blocked' ? '启动已阻止' : '已停止'}
                              </Pill>
                            </td>
                            <td className="align-right">
                              <button
                                className="icon-button"
                                aria-label={`启动 ${e.name}`}
                                disabled={actionBusy}
                                onClick={() =>
                                  void mutate(
                                    () => api(`/exits/${e.id}/start`, 'POST'),
                                    '启动请求已发送',
                                  )
                                }
                              >
                                <Power size={17} />
                              </button>
                              <button
                                className="icon-button"
                                aria-label={`查看 ${e.name}`}
                                onClick={() => setDetail(e)}
                              >
                                <DotsThree size={23} />
                              </button>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
                <div className="panel-footer">
                  <span>
                    <Info size={14} />
                    只监听回环地址；公网访问未开启。
                  </span>
                  <span>{filtered.length} 个出口</span>
                </div>
              </section>
              {page === 'overview' && (
                <div className="bottom-grid">
                  <section className="panel">
                    <div className="section-heading compact">
                      <h2>最近事件</h2>
                      <button className="text-button" onClick={() => setPage('events')}>
                        查看全部
                        <ArrowRight size={15} />
                      </button>
                    </div>
                    <EventList events={events.slice(0, 4)} />
                  </section>
                  <section className="next-panel">
                    <div className="next-icon">
                      <ShieldCheck size={26} />
                    </div>
                    <span className="eyebrow">BUILT WITH BOUNDARIES</span>
                    <h3>安全能力，要经过验证。</h3>
                    <p>不会用一个绿色状态，代替实际的隧道、DNS 和断线保护测试。</p>
                    <button className="text-button" onClick={() => setPage('system')}>
                      查看环境诊断
                      <ArrowRight size={16} />
                    </button>
                  </section>
                </div>
              )}
            </>
          )}
          {page === 'nodes' && (
            <section className="panel">
              <div className="section-heading">
                <div>
                  <h2>
                    VPN Gate 候选库 <span className="count">{nodes.length}</span>
                  </h2>
                  <p>仅从官方 HTTPS 地址同步。失败保留原缓存，最多每分钟同步一次。</p>
                </div>
                <Pill>来源：VPN Gate</Pill>
              </div>
              <div className="table-toolbar">
                <div className="search">
                  <MagnifyingGlass size={18} />
                  <input
                    aria-label="搜索节点"
                    placeholder="搜索国家、IP 或节点名称…"
                    value={nodeQuery}
                    onChange={(e) => {
                      setNodeQuery(e.target.value);
                      setNodeLimit(50);
                    }}
                  />
                </div>
                <span className="muted small">住宅分类与实测：尚未接入</span>
              </div>
              {nodes.length === 0 ? (
                <Empty
                  icon={Database}
                  title="节点库还是空的"
                  detail="同步将访问 VPN Gate 官方列表，不会自动连接其中的节点。"
                  action={
                    <button
                      className="button"
                      onClick={() => void refreshNodes()}
                      disabled={refreshing}
                    >
                      {refreshing ? <Spinner /> : <ArrowClockwise size={17} />}同步官方节点
                    </button>
                  }
                />
              ) : (
                <>
                  <div className="table-scroll">
                    <table>
                      <thead>
                        <tr>
                          <th>节点</th>
                          <th>国家</th>
                          <th>列表延迟</th>
                          <th>类型 / 可用性</th>
                          <th>同步时间</th>
                        </tr>
                      </thead>
                      <tbody>
                        {filteredNodes.slice(0, nodeLimit).map((n) => (
                          <tr key={n.id}>
                            <td>
                              <strong className="small">{n.hostname}</strong>
                              <code className="subline">{n.ip}</code>
                            </td>
                            <td>
                              {countries[n.country] ?? n.country_name}{' '}
                              <span className="muted">{n.country}</span>
                            </td>
                            <td>
                              <code>{n.advertised_ping > 0 ? `${n.advertised_ping} ms` : '—'}</code>
                              <small className="subline">非本机实测</small>
                            </td>
                            <td>
                              <Pill>类型未知</Pill>
                              <small className="subline">未测试连通性</small>
                            </td>
                            <td className="small muted">{timestamp(n.fetched_at)}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                  <div className="panel-footer">
                    <span>
                      {Math.min(nodeLimit, filteredNodes.length)} / {filteredNodes.length} 条候选
                    </span>
                    {nodeLimit < filteredNodes.length && (
                      <button className="text-button" onClick={() => setNodeLimit((n) => n + 50)}>
                        加载更多
                      </button>
                    )}
                  </div>
                </>
              )}
            </section>
          )}
          {page === 'events' && (
            <section className="panel">
              <div className="section-heading">
                <div>
                  <h2>操作与系统事件</h2>
                  <p>最多显示最近 100 条，数据库保留最近 500 条。不记录密码和访问内容。</p>
                </div>
                <Pill>审计记录</Pill>
              </div>
              <EventList events={events} />
            </section>
          )}
          {page === 'system' && (
            <>
              <section className="panel">
                <div className="section-heading">
                  <div>
                    <h2>环境诊断</h2>
                    <p>只读检查，不安装依赖、不修改路由或防火墙。</p>
                  </div>
                  <Pill>{diagnostics?.platform ?? '检测中'}</Pill>
                </div>
                <div className="checks">
                  {diagnostics?.checks.map((c) => (
                    <div className="check-row" key={c.id}>
                      {c.status === 'ok' ? (
                        <CheckCircle className="teal-text" size={23} />
                      ) : (
                        <ShieldWarning className="amber-text" size={23} />
                      )}
                      <div>
                        <strong>{c.name}</strong>
                        <p>{c.detail}</p>
                      </div>
                      <Pill tone={c.status === 'ok' ? 'teal' : 'amber'}>
                        {c.status === 'ok' ? '已检测' : '不可用'}
                      </Pill>
                    </div>
                  ))}
                </div>
              </section>
              <div className="bottom-grid">
                <section className="panel settings-card">
                  <h2>当前发布范围</h2>
                  <ul className="feature-list">
                    <li>
                      <Check size={17} />
                      账号登录与安全会话
                    </li>
                    <li>
                      <Check size={17} />
                      SQLite 配置与审计持久化
                    </li>
                    <li>
                      <Check size={17} />
                      出口配置和独立凭据
                    </li>
                    <li>
                      <Check size={17} />
                      官方节点列表解析与校验
                    </li>
                    <li>
                      <Check size={17} />
                      隔离计划预览与独立 Linux 沙箱助手
                    </li>
                    <li className="muted">
                      <Clock size={17} />
                      Linux 真实隧道与防泄漏验收：待完成
                    </li>
                    <li className="muted">
                      <Clock size={17} />
                      热备、Xray、订阅：后续版本
                    </li>
                  </ul>
                </section>
                <section className="panel settings-card">
                  <h2>安全默认值</h2>
                  <dl className="settings-list">
                    <div>
                      <dt>管理监听</dt>
                      <dd>仅回环地址</dd>
                    </div>
                    <div>
                      <dt>初始化密码</dt>
                      <dd>随机生成，无默认口令</dd>
                    </div>
                    <div>
                      <dt>会话有效期</dt>
                      <dd>8 小时</dd>
                    </div>
                    <div>
                      <dt>敏感操作</dt>
                      <dd>会话 + CSRF 校验</dd>
                    </div>
                    <div>
                      <dt>数据面状态</dt>
                      <dd>未启动任何代理</dd>
                    </div>
                  </dl>
                </section>
              </div>
            </>
          )}
          <footer className="page-footer">
            <span>
              Miku Gate <b>·</b> 让连接更有条理
            </span>
            <span>
              控制台开发版 <b>·</b> 无活动 VPN 数据面
            </span>
          </footer>
        </main>
      </div>
      {form && (
        <ExitForm
          current={form === 'new' ? undefined : form}
          submit={saveExit}
          close={() => setForm(null)}
        />
      )}
      {secret && <Secret {...secret} close={() => setSecret(null)} notify={setToast} />}
      {detail && (
        <Modal title={detail.name} close={() => setDetail(null)}>
          <div className="modal-form">
            <div className="detail-top">
              <span className="country-code">{detail.country}</span>
              <div>
                <strong>{countries[detail.country] ?? detail.country}</strong>
                <p className="muted">{policies[detail.policy]}</p>
              </div>
            </div>
            <div className="notice">
              <Info size={20} />
              <p>{exits.find((e) => e.id === detail.id)?.reason ?? detail.reason}</p>
            </div>
            <dl className="credential-list">
              <div>
                <dt>入口地址</dt>
                <dd>
                  <code>
                    {detail.listen}:{detail.port}
                  </code>
                </dd>
              </div>
              <div>
                <dt>用户名</dt>
                <dd>
                  <code>{detail.username}</code>
                </dd>
              </div>
              <div>
                <dt>出口 ID</dt>
                <dd>
                  <code>{detail.id}</code>
                </dd>
              </div>
              <div>
                <dt>创建时间</dt>
                <dd>{timestamp(detail.created_at)}</dd>
              </div>
            </dl>
            <NetworkPlanView key={detail.id} exitID={detail.id} />
            <div className="detail-actions">
              <button
                className="button"
                onClick={() => {
                  setDetail(null);
                  setForm(detail);
                }}
              >
                <SlidersHorizontal size={17} />
                编辑配置
              </button>
              <button className="button" disabled={actionBusy} onClick={() => void rotate(detail)}>
                <Key size={17} />
                重置凭据
              </button>
              <button
                className="button"
                disabled={actionBusy}
                onClick={() =>
                  void mutate(() => api(`/exits/${detail.id}/stop`, 'POST'), '已停止出口')
                }
              >
                <Pause size={17} />
                停止
              </button>
              <button
                className="button danger"
                onClick={() => {
                  setDetail(null);
                  setDeleting(detail);
                }}
              >
                <Trash size={17} />
                删除配置
              </button>
            </div>
          </div>
        </Modal>
      )}
      {deleting && (
        <Modal
          title="删除出口配置？"
          close={() => {
            if (!actionBusy) setDeleting(null);
          }}
        >
          <div className="modal-form">
            <p>
              将删除 <strong>{deleting.name}</strong> 的入口预留、策略和凭据配置。操作记录会保留。
            </p>
            <p className="muted">
              隔离沙箱不会自动回收；请先显式清理。启用 IPC
              时会检查实际状态，无法确认已清理则拒绝删除；未启用时请先用独立 CLI 清理。
            </p>
            <div className="modal-actions">
              <button className="button" disabled={actionBusy} onClick={() => setDeleting(null)}>
                取消
              </button>
              <button
                className="button danger"
                disabled={actionBusy}
                onClick={() =>
                  void mutate(async () => {
                    await api(`/exits/${deleting.id}`, 'DELETE');
                    setDeleting(null);
                  }, '出口配置已删除')
                }
              >
                <Trash size={17} />
                确认删除
              </button>
            </div>
          </div>
        </Modal>
      )}
      {toast && (
        <div className="toast" role="status">
          <Info size={19} />
          <span>{toast}</span>
          <button aria-label="关闭提示" onClick={() => setToast('')}>
            <X size={16} />
          </button>
        </div>
      )}
    </div>
  );
}
function EventList({ events }: { events: AuditEvent[] }) {
  return events.length ? (
    <div className="event-list">
      {events.map((e) => (
        <div className="event-row" key={e.id}>
          <span className={`event-symbol ${e.level === 'warning' ? 'warning' : ''}`}>
            {e.level === 'warning' ? <Warning size={16} /> : <Check size={16} />}
          </span>
          <div>
            <strong>{e.message}</strong>
            <small>{e.kind}</small>
          </div>
          <time>{timestamp(e.created_at)}</time>
        </div>
      ))}
    </div>
  ) : (
    <Empty icon={Clock} title="还没有事件" detail="配置变更和系统操作会记录在这里。" />
  );
}
