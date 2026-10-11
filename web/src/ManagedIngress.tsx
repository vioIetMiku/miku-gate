import { useEffect, useState } from 'react';
import { CircleNotch, Info } from '@phosphor-icons/react';
import { api, timestamp, type Exit } from './api';

type Template = {
  id: string;
  name: string;
  cores: string[];
  needs_certificate: boolean;
  needs_reality_target: boolean;
};
type Target = { hostname: string; port: number; hint: string };
type Preview = {
  request: { core: string; template: string; address: string; port: number; server_name: string };
  needs_certificate: boolean;
  needs_reality_target: boolean;
  applied: boolean;
  core_validated: boolean;
  warnings: string[];
};
type Certificate = {
  hostname: string;
  private_key_returned: boolean;
  selected_provider?: string;
  findings: {
    provider: string;
    state: string;
    certificate?: { not_after: string; leaf_sha256: string };
  }[];
};
type CheckedTarget = {
  hostname: string;
  address: string;
  tls_version: string;
  alpn: string;
  curve: string;
  checked_at: string;
  suitable: boolean;
  network_path: string;
};
type SavedIngress = {
  configured: boolean;
  exit_id: string;
  request: { core: string; template: string; address: string; port: number; server_name: string };
  applied: boolean;
  updated_at: string;
};
type CoreState = {
  phase: string;
  core?: string;
  listen?: string;
  pid?: number;
  core_validated: boolean;
  listener_owned: boolean;
  data_verified: boolean;
  reason: string;
};
export function ManagedIngress() {
  const [exits, setExits] = useState<Exit[]>([]);
  const [exitId, setExitId] = useState('');
  const [exitPassword, setExitPassword] = useState('');
  const [confirmSave, setConfirmSave] = useState(false);
  const [saved, setSaved] = useState<SavedIngress | null>(null);
  const [runtime, setRuntime] = useState<CoreState | null>(null);
  const [client, setClient] = useState<{ uri: string; client_config: string } | null>(null);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [candidates, setCandidates] = useState<Target[]>([]);
  const [core, setCore] = useState('xray');
  const [template, setTemplate] = useState('vless-tls-vision');
  const [address, setAddress] = useState('');
  const [port, setPort] = useState('8443');
  const [domain, setDomain] = useState('');
  const [target, setTarget] = useState('www.microsoft.com');
  const [custom, setCustom] = useState('');
  const [confirmRead, setConfirmRead] = useState(false);
  const [confirmNetwork, setConfirmNetwork] = useState(false);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [cert, setCert] = useState<Certificate | null>(null);
  const [checked, setChecked] = useState<CheckedTarget | null>(null);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const isReality = template === 'vless-reality-vision';
  const targetHost = target === 'custom' ? custom : target;
  useEffect(() => {
    let active = true;
    Promise.all([
      api<Template[]>('/ingress/templates'),
      api<Target[]>('/ingress/reality/candidates'),
      api<Exit[]>('/exits'),
    ])
      .then(([t, c, x]) => {
        if (active) {
          setExits(x);
          setTemplates(t);
          setCandidates(c);
        }
      })
      .catch((e) => {
        if (active) setError((e as Error).message);
      });
    return () => {
      active = false;
    };
  }, []);
  useEffect(() => {
    let active = true;
    setSaved(null);
    setRuntime(null);
    setClient(null);
    setExitPassword('');
    setConfirmSave(false);
    if (exitId)
      Promise.all([
        api<SavedIngress>(`/exits/${exitId}/ingress`),
        api<CoreState>(`/exits/${exitId}/ingress/runtime`),
      ])
        .then(([v, state]) => {
          if (active) {
            setSaved(v);
            setRuntime(state);
          }
        })
        .catch((e) => {
          if (active) setError((e as Error).message);
        });
    return () => {
      active = false;
    };
  }, [exitId]);
  const request = () => ({
    core,
    template,
    address,
    listen: '0.0.0.0',
    port: Number(port),
    server_name: isReality ? targetHost : domain,
    reality_hostname: isReality ? targetHost : '',
  });
  async function refreshRuntime() {
    if (!exitId) return;
    setSaved(await api<SavedIngress>(`/exits/${exitId}/ingress`));
    setRuntime(await api<CoreState>(`/exits/${exitId}/ingress/runtime`));
  }
  async function action(kind: 'save' | 'start' | 'stop' | 'client' | 'remove' | 'status') {
    if (!exitId) return;
    setBusy(kind);
    setError('');
    try {
      const prefix = `/exits/${exitId}/ingress`;
      if (kind === 'save') {
        setSaved(
          await api<SavedIngress>(prefix, 'PUT', {
            request: request(),
            exit_password: exitPassword,
            confirm: confirmSave,
            confirm_read: confirmRead,
            confirm_network: confirmNetwork,
          }),
        );
        setExitPassword('');
        setClient(null);
      } else if (kind === 'start') {
        setRuntime(await api<CoreState>(prefix + '/start', 'POST', { confirm: true }));
      } else if (kind === 'stop') {
        await api(prefix + '/stop', 'POST', {});
        await refreshRuntime();
      } else if (kind === 'client') {
        setClient(await api(prefix + '/client', 'POST', { confirm: true }));
      } else if (kind === 'remove') {
        await api(prefix, 'DELETE');
        setClient(null);
        await refreshRuntime();
      } else await refreshRuntime();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy('');
    }
  }
  function clear() {
    setPreview(null);
    setError('');
  }
  async function run(kind: 'preview' | 'certificate' | 'target') {
    setBusy(kind);
    setError('');
    try {
      if (kind === 'preview')
        setPreview(
          await api<Preview>('/ingress/preview', 'POST', {
            core,
            template,
            address,
            listen: '0.0.0.0',
            port: Number(port),
            server_name: isReality ? targetHost : domain,
            reality_hostname: isReality ? targetHost : '',
          }),
        );
      else if (kind === 'certificate') {
        setCert(null);
        setCert(
          await api<Certificate>('/ingress/certificate', 'POST', {
            hostname: domain,
            confirm_read: confirmRead,
          }),
        );
      } else {
        setChecked(null);
        setChecked(
          await api<CheckedTarget>('/ingress/reality/probe', 'POST', {
            hostname: targetHost,
            confirm_network: confirmNetwork,
          }),
        );
      }
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy('');
    }
  }
  const disabled = busy !== '';
  return (
    <section className="panel managed-ingress" aria-labelledby="managed-ingress-title">
      <div className="section-heading">
        <div>
          <h2 id="managed-ingress-title">新建受管入口</h2>
          <p>不使用已有节点时，独立选择核心与加密方式。先预览，再生成私有配置。</p>
        </div>
        <Info size={21} />
      </div>
      <div className="managed-content">
        <label>
          绑定逻辑出口
          <select value={exitId} disabled={disabled} onChange={(e) => setExitId(e.target.value)}>
            <option value="">仅预览（未绑定出口）</option>
            {exits.map((e) => (
              <option key={e.id} value={e.id}>
                {e.name} · 本机端口 {e.port}
              </option>
            ))}
          </select>
        </label>
        {exitId && (
          <p className="muted">
            先保存独立入口，再启动健康的逻辑出口，最后启动加密入口。不接管已有面板，也不自动开放防火墙。
          </p>
        )}

        <div className="managed-form-grid">
          <label>
            受管核心
            <select
              value={core}
              disabled={disabled}
              onChange={(e) => {
                setCore(e.target.value);
                clear();
              }}
            >
              <option value="xray">Xray</option>
              <option value="sing-box">sing-box</option>
            </select>
          </label>
          <label>
            入口加密方式
            <select
              value={template}
              disabled={disabled}
              onChange={(e) => {
                setTemplate(e.target.value);
                clear();
                setConfirmRead(false);
                setConfirmNetwork(false);
              }}
            >
              {templates.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            客户端连接地址
            <input
              value={address}
              placeholder="VPS IP 或你的域名"
              disabled={disabled}
              onChange={(e) => {
                setAddress(e.target.value);
                clear();
              }}
            />
          </label>
          <label>
            入口监听端口
            <input
              value={port}
              inputMode="numeric"
              disabled={disabled}
              onChange={(e) => {
                setPort(e.target.value);
                clear();
              }}
            />
          </label>
        </div>
        {!isReality ? (
          <>
            <label>
              TLS 证书域名
              <input
                value={domain}
                placeholder="例如 gate.example.com"
                disabled={disabled}
                onChange={(e) => {
                  setDomain(e.target.value);
                  setCert(null);
                  setConfirmRead(false);
                  clear();
                }}
              />
            </label>
            <label className="managed-confirm">
              <input
                type="checkbox"
                checked={confirmRead}
                disabled={disabled}
                onChange={(e) => setConfirmRead(e.target.checked)}
              />
              我确认后端可查找此域名的现有证书，校验信任链和公私钥匹配；不返回私钥。
            </label>
            <button
              className="button secondary"
              disabled={!confirmRead || !domain || disabled}
              onClick={() => void run('certificate')}
            >
              {busy === 'certificate' && <CircleNotch className="spin" size={17} />}自动查找域名证书
            </button>
            {cert && (
              <div className="callout">
                <Info size={18} />
                <div>
                  <strong>
                    {cert.selected_provider
                      ? '已找到验证通过的证书'
                      : '未找到当前账号可用的已验证证书'}
                  </strong>
                  <p>
                    {cert.findings
                      .map(
                        (f) =>
                          `${f.provider}：${f.state === 'verified' ? '验证通过' : f.state === 'not_found' ? '未找到' : f.state === 'unverified' ? '验证失败' : '无权读取或安全路径不满足'}`,
                      )
                      .join('；')}
                  </p>
                  <p>私钥未返回；root 私有文件后续由授权安装助手读取，不会擅自开放文件权限。</p>
                </div>
              </div>
            )}
          </>
        ) : (
          <>
            <label>
              REALITY 目标
              <select
                value={target}
                disabled={disabled}
                onChange={(e) => {
                  setTarget(e.target.value);
                  setChecked(null);
                  setConfirmNetwork(false);
                  clear();
                }}
              >
                {candidates.map((c) => (
                  <option key={c.hostname} value={c.hostname}>
                    {c.hostname}:443 · 待检测
                  </option>
                ))}
                <option value="custom">自定义域名</option>
              </select>
            </label>
            {target === 'custom' && (
              <label>
                自定义 REALITY 域名
                <input
                  value={custom}
                  disabled={disabled}
                  placeholder="只填域名，不填URL或IP"
                  onChange={(e) => {
                    setCustom(e.target.value);
                    setChecked(null);
                    setConfirmNetwork(false);
                    clear();
                  }}
                />
              </label>
            )}
            <p className="muted">
              目标不是你的管理域名；候选不保证可用。只检查一个443端点，不遍历端口。IP位置不等于经营者归属。
            </p>
            <label className="managed-confirm">
              <input
                type="checkbox"
                checked={confirmNetwork}
                disabled={disabled}
                onChange={(e) => setConfirmNetwork(e.target.checked)}
              />
              我同意从 VPS 主机网络探测所选目标的 TLS 1.3 / X25519 / h2 和系统证书；这不是 VPN
              出口探测。
            </label>
            <button
              className="button secondary"
              disabled={!confirmNetwork || !targetHost || disabled}
              onClick={() => void run('target')}
            >
              {busy === 'target' && <CircleNotch className="spin" size={17} />}检测 REALITY 目标
            </button>
            {checked && (
              <div className="callout">
                <Info size={18} />
                <div>
                  <strong>本次目标检测通过</strong>
                  <p>
                    {checked.hostname} · {checked.address} · TLS {checked.tls_version} /{' '}
                    {checked.curve} / {checked.alpn} · {timestamp(checked.checked_at)}
                  </p>
                  <p>仅是短时TLS适配证据；生成时重新验证，不把浏览器结果当授权。</p>
                </div>
              </div>
            )}
          </>
        )}
        <div className="managed-actions">
          <button
            className="button primary"
            disabled={disabled || !address || templates.length === 0}
            onClick={() => void run('preview')}
          >
            {busy === 'preview' && <CircleNotch className="spin" size={17} />}预览受管入口计划
          </button>
          <span className="muted">不启动、不开放端口、不覆盖已有核心。</span>
        </div>
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        {preview && (
          <div className="managed-preview" aria-live="polite">
            <strong>
              计划：{preview.request.core} ·{' '}
              {templates.find((t) => t.id === preview.request.template)?.name}
            </strong>
            <p>
              {preview.request.address}:{preview.request.port} · 服务器名称{' '}
              {preview.request.server_name}
            </p>
            <p>TCP → 本机认证 Miku 出口 · UDP 阻断 · 尚未应用 / 尚未核心校验</p>
            <ul className="inventory-warnings">
              {preview.warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          </div>
        )}

        {exitId && (
          <div className="managed-binding">
            <label>
              逻辑出口入口密码
              <input
                type="password"
                value={exitPassword}
                disabled={disabled}
                autoComplete="off"
                placeholder="创建出口时的一次性密码，服务端核对后加密保存"
                onChange={(e) => setExitPassword(e.target.value)}
              />
            </label>
            <label className="managed-confirm">
              <input
                type="checkbox"
                checked={confirmSave}
                disabled={disabled}
                onChange={(e) => setConfirmSave(e.target.checked)}
              />
              我确认保存或替换此独立受管入口（需入口和出口均停止）；不修改已有面板。
            </label>
            <button
              className="button primary"
              disabled={disabled || !confirmSave || !exitPassword || !address}
              onClick={() => void action('save')}
            >
              保存并绑定入口
            </button>
            {saved?.configured && (
              <>
                <p>
                  已保存：{saved.request.core} · {saved.request.template} · {saved.request.address}:
                  {saved.request.port}（保存不等于运行）
                </p>
                <p>
                  运行状态：{runtime?.phase ?? '未知'} · {runtime?.reason}
                </p>
                <p>
                  核心校验：{runtime?.core_validated ? '通过' : '未确认'} · 监听进程归属：
                  {runtime?.listener_owned ? '已核验' : '未确认'} · 客户端业务：
                  {runtime?.data_verified ? '已验证' : '尚未逐条验证'}
                </p>
                <div className="managed-actions">
                  <button
                    className="button secondary"
                    disabled={disabled || runtime?.phase === 'running'}
                    onClick={() => void action('start')}
                  >
                    启动已保存入口
                  </button>
                  <button
                    className="button secondary"
                    disabled={disabled}
                    onClick={() => void action('stop')}
                  >
                    停止受管入口
                  </button>
                  <button
                    className="button secondary"
                    disabled={disabled}
                    onClick={() => void action('status')}
                  >
                    刷新入口状态
                  </button>
                  <button
                    className="button secondary"
                    disabled={disabled}
                    onClick={() => void action('client')}
                  >
                    显示客户端导入（含凭据）
                  </button>
                  <button
                    className="button secondary"
                    disabled={disabled || runtime?.phase === 'running'}
                    onClick={() => void action('remove')}
                  >
                    移除入口配置
                  </button>
                </div>
                {client && (
                  <details open className="managed-client">
                    <summary>客户端材料（敏感，仅自行导入；不含服务器私钥或Miku出口密码）</summary>
                    <textarea readOnly aria-label="入口客户端分享链接" value={client.uri} />
                    <textarea readOnly aria-label="入口客户端配置" value={client.client_config} />
                    <button className="button secondary" onClick={() => setClient(null)}>
                      隐藏客户端材料
                    </button>
                  </details>
                )}
              </>
            )}
          </div>
        )}
        <p className="muted">
          常驻核心由管理员serve参数明确指定root所有的受信任程序，以普通UID运行。重启不自动启动入口；新core/API功能仍需完整公共链路和安装向导验收。
        </p>
      </div>
    </section>
  );
}
