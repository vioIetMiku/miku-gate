import { useEffect, useState, type FormEvent } from 'react';
import {
  ArrowClockwise,
  CircleNotch,
  Info,
  ShieldCheck,
  ShieldWarning,
  Trash,
} from '@phosphor-icons/react';
import { IPIntelligence } from './IPIntelligence';
import { Modal } from './Modal';
import {
  api,
  hasRuntime,
  runtimeLabels,
  runtimeTone,
  timestamp,
  type Exit,
  type UpstreamConfig,
  type UpstreamInput,
  type RuntimeState,
  type VPNBinding,
} from './api';

export function UpstreamSettings({
  exit,
  close,
  saved,
}: {
  exit: Exit;
  close: () => void;
  saved: () => Promise<void>;
}) {
  const [original, setOriginal] = useState<UpstreamConfig | null>(null);
  const [kind, setKind] = useState<UpstreamConfig['kind']>('socks5');
  const [host, setHost] = useState('');
  const [port, setPort] = useState('1080');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [tlsName, setTLSName] = useState('');
  const [dns, setDNS] = useState<'cloudflare' | 'google'>('cloudflare');
  const [loopback, setLoopback] = useState(false);
  const [plaintext, setPlaintext] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [hasVPN, setHasVPN] = useState(false);
  const [replaceVPN, setReplaceVPN] = useState(false);
  const locked = hasRuntime(exit.status);
  useEffect(() => {
    let alive = true;
    Promise.all([
      api<UpstreamConfig>(`/exits/${exit.id}/upstream`),
      api<VPNBinding>(`/exits/${exit.id}/vpn`),
    ])
      .then(([value, vpn]) => {
        if (!alive) return;
        setOriginal(value);
        setHasVPN(vpn.configured);
        if (value.configured) {
          setKind(value.kind);
          setHost(value.host);
          setPort(String(value.port));
          setUsername(value.username);
          setTLSName(value.tls_name);
          setDNS(value.dns);
          setLoopback(value.allow_loopback);
          setPlaintext(value.allow_plaintext);
        }
      })
      .catch((err) => {
        if (alive) setError((err as Error).message);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [exit.id]);
  async function save(event: FormEvent) {
    event.preventDefault();
    if (busy || locked) return;
    setBusy(true);
    setError('');
    const input: UpstreamInput = {
      kind,
      host,
      port: Number(port),
      username,
      password,
      reuse_password: !!original?.has_password && password === '' && username !== '',
      tls_name: kind === 'https' ? tlsName : '',
      dns,
      allow_loopback: loopback,
      allow_plaintext: plaintext,
      replace_vpn: replaceVPN,
    };
    try {
      await api(`/exits/${exit.id}/upstream`, 'PUT', input);
      setPassword('');
      await saved();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function remove() {
    if (busy || locked) return;
    setBusy(true);
    setError('');
    try {
      await api(`/exits/${exit.id}/upstream`, 'DELETE');
      await saved();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  function choose(next: UpstreamConfig['kind']) {
    setKind(next);
    if (['1080', '8080', '443'].includes(port))
      setPort(next === 'socks5' ? '1080' : next === 'http' ? '8080' : '443');
    if (next !== 'https') setTLSName('');
  }
  return (
    <Modal
      title={`代理上游 · ${exit.name}`}
      close={() => {
        if (!busy) close();
      }}
    >
      <form className="modal-form upstream-form" onSubmit={save}>
        <div className="notice">
          <Info size={20} />
          <p>
            这是出口使用的代理服务器，不是客户端入口。VPN Gate 的 OpenVPN 配置不能填成 SOCKS/HTTP
            上游；OpenVPN 请用出口的“选择VPN”入口，并先授权独立执行器。
          </p>
        </div>
        {loading ? (
          <p role="status">
            <CircleNotch className="spin" /> 读取上游配置…
          </p>
        ) : (
          <>
            {locked && (
              <div className="inline-error" role="status">
                此出口正在运行或停止中。请先停止，再更换上游或凭据。
              </div>
            )}
            <fieldset className="upstream-fields" disabled={busy || locked || !original}>
              <div className="form-grid">
                <label>
                  上游协议
                  <select
                    value={kind}
                    onChange={(e) => choose(e.target.value as UpstreamConfig['kind'])}
                  >
                    <option value="socks5">SOCKS5</option>
                    <option value="http">HTTP CONNECT</option>
                    <option value="https">HTTPS CONNECT（加密）</option>
                  </select>
                </label>
                <label>
                  业务 DNS
                  <select
                    value={dns}
                    onChange={(e) => setDNS(e.target.value as 'cloudflare' | 'google')}
                  >
                    <option value="cloudflare">Cloudflare · 经上游 DoH</option>
                    <option value="google">Google · 经上游 DoH</option>
                  </select>
                </label>
              </div>
              <div className="form-grid">
                <label>
                  上游地址
                  <input
                    value={host}
                    onChange={(e) => setHost(e.target.value)}
                    required
                    maxLength={253}
                    placeholder="proxy.example.com 或 127.0.0.1"
                    autoComplete="off"
                  />
                </label>
                <label>
                  上游端口
                  <input
                    type="number"
                    min={1}
                    max={65535}
                    value={port}
                    onChange={(e) => setPort(e.target.value)}
                    required
                  />
                </label>
              </div>
              <div className="form-grid">
                <label>
                  上游用户名（可选）
                  <input
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    maxLength={255}
                    autoComplete="off"
                  />
                </label>
                <label>
                  上游密码（可选）
                  <input
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    maxLength={255}
                    autoComplete="new-password"
                    placeholder={
                      original?.has_password ? '已加密保存；留空保留' : '与用户名同时填写或留空'
                    }
                  />
                </label>
              </div>
              {original?.has_password && (
                <p className="field-note">
                  不会回传旧密码。留空仅对相同地址、协议、端口、TLS身份及用户名保留原凭据；更换目标请重新输入。清空用户名和密码会移除认证。
                </p>
              )}
              {kind === 'https' && (
                <label>
                  TLS 服务器名（可选）
                  <input
                    value={tlsName}
                    onChange={(e) => setTLSName(e.target.value)}
                    maxLength={253}
                    placeholder="证书对应域名；留空使用上游地址"
                  />
                  <span className="field-note">始终校验证书，不提供跳过验证选项。</span>
                </label>
              )}
              <label className="option-check">
                <input
                  type="checkbox"
                  checked={loopback}
                  onChange={(e) => setLoopback(e.target.checked)}
                />
                <span>
                  允许我明确指定的本机 127.0.0.1 上游（端口须
                  ≥1024）。域名解析到私网或元数据地址仍被拒绝。
                </span>
              </label>
              {kind !== 'https' && (
                <label className="option-check">
                  <input
                    type="checkbox"
                    checked={plaintext}
                    onChange={(e) => setPlaintext(e.target.checked)}
                  />
                  <span>
                    我理解远程 HTTP/SOCKS 链路本身不加密，可能暴露上游凭据与非 HTTPS 业务；建议用
                    HTTPS 或可信本机链路。
                  </span>
                </label>
              )}
              {hasVPN && (
                <label className="option-check">
                  <input
                    type="checkbox"
                    checked={replaceVPN}
                    onChange={(e) => setReplaceVPN(e.target.checked)}
                  />
                  <span>我确认用此代理上游替换当前OpenVPN绑定。未确认不会移除现有配置。</span>
                </label>
              )}
              <p className="field-note">
                业务 DNS 与 TCP 只经此上游；失败不直连。上游地址自身若是域名，会由本机解析并固定公网
                IP。配置地区不是实测出口所在地。
              </p>
            </fieldset>
          </>
        )}
        {error && (
          <div className="inline-error" role="alert">
            {error}
          </div>
        )}
        {confirmRemove && (
          <div className="notice">
            <ShieldWarning size={20} />
            <p>确认移除上游？出口、端口和入口凭据保留，但不能继续使用这个代理出口。</p>
            <button
              className="button danger"
              type="button"
              disabled={busy || locked}
              onClick={() => void remove()}
            >
              确认移除上游
            </button>
          </div>
        )}
        <div className="modal-actions">
          {original?.configured && (
            <button
              type="button"
              className="button danger"
              disabled={busy || locked}
              onClick={() => setConfirmRemove(!confirmRemove)}
            >
              <Trash size={16} />
              移除上游
            </button>
          )}
          <button className="button" type="button" onClick={close} disabled={busy}>
            关闭
          </button>
          <button
            className="button primary"
            disabled={busy || locked || loading || !original || (hasVPN && !replaceVPN)}
          >
            {busy ? <CircleNotch className="spin" size={17} /> : '保存上游'}
          </button>
        </div>
      </form>
    </Modal>
  );
}

export function RuntimeView({ exitID, changed }: { exitID: string; changed: () => Promise<void> }) {
  const [state, setState] = useState<RuntimeState | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [intelligenceOpen, setIntelligenceOpen] = useState(false);
  useEffect(() => {
    let alive = true;
    const read = () =>
      api<RuntimeState>(`/exits/${exitID}/runtime`)
        .then((value) => {
          if (alive) {
            setState(value);
            setError('');
          }
        })
        .catch((err) => {
          if (alive) setError((err as Error).message);
        });
    void read();
    const timer = window.setInterval(() => {
      if (!document.hidden) void read();
    }, 10000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [exitID]);
  async function probe() {
    setBusy(true);
    setError('');
    try {
      setState(await api<RuntimeState>(`/exits/${exitID}/probe`, 'POST'));
      await changed();
    } catch (err) {
      setError((err as Error).message);
      try {
        setState(await api<RuntimeState>(`/exits/${exitID}/runtime`));
        await changed();
      } catch {
        /* retain the first actionable error */
      }
    } finally {
      setBusy(false);
    }
  }
  const checked = state?.checked_at && !state.checked_at.startsWith('0001-');
  return (
    <section className="runtime-card" aria-label="真实代理运行状态">
      <div className="runtime-heading">
        <h3>真实运行状态</h3>
        {state && (
          <span className={`pill ${runtimeTone(state.phase)}`}>{runtimeLabels[state.phase]}</span>
        )}
      </div>
      {state ? (
        <>
          <dl className="credential-list">
            <div>
              <dt>实际监听</dt>
              <dd>{state.proxy_listening ? state.listen : '未监听'}</dd>
            </div>
            <div>
              <dt>出站路径</dt>
              <dd>{state.proof.kind ? state.proof.kind.toUpperCase() + ' 上游' : '未建立'}</dd>
            </div>
            <div>
              <dt>出口 DNS</dt>
              <dd>{state.proof.dns_verified ? '已通过指定出口验证' : '未验证 / 当前不可用'}</dd>
            </div>
            <div>
              <dt>上次探测</dt>
              <dd>
                {checked ? `${timestamp(state.checked_at!)} · ${state.check_ms}ms` : '尚未探测'}
              </dd>
            </div>
            <div>
              <dt>公网出口 IP</dt>
              <dd>{state.proof.exit_ip || '尚未测量（不使用配置地区冒充）'}</dd>
            </div>
          </dl>
          <button
            type="button"
            className="text-button"
            disabled={!state.healthy || !state.proof.exit_ip}
            onClick={() => setIntelligenceOpen(true)}
          >
            查询实测出口归属 / ASN / 风险
          </button>
          <p className="field-note">
            {state.proof.protection_mode === 'proxy_only'
              ? '应用层只经代理上游转发；不是OpenVPN隧道或内核隔离的证明。'
              : state.proof.protection_mode === 'namespace' && state.proof.kernel_verified
                ? '已核对本会话的独立namespace、TUN接口与内核规则；业务通过低权限worker。'
                : '尚无当前有效的内核保护证明。'}
          </p>
          <div className="runtime-actions">
            <span>
              {state.healthy ? <ShieldCheck size={18} /> : <ShieldWarning size={18} />}{' '}
              {state.reason}
            </span>
            <button
              className="button"
              type="button"
              disabled={busy || !state.proxy_listening}
              onClick={() => void probe()}
            >
              {busy ? <CircleNotch className="spin" size={16} /> : <ArrowClockwise size={16} />}
              检测连通性
            </button>
          </div>
        </>
      ) : (
        <p className="muted" role="status">
          读取实际状态…
        </p>
      )}
      {error && (
        <div className="inline-error" role="alert">
          {error}
        </div>
      )}
      {intelligenceOpen && (
        <IPIntelligence
          selection={{ kind: 'exit', id: exitID, name: '当前实测公网出口' }}
          close={() => setIntelligenceOpen(false)}
        />
      )}
    </section>
  );
}
