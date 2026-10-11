import { useEffect, useState } from 'react';
import { CircleNotch, Info } from '@phosphor-icons/react';
import { api, timestamp } from './api';

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
export function ManagedIngress() {
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
    ])
      .then(([t, c]) => {
        if (active) {
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
        <p className="muted">
          本阶段通过本机 ingress managed 命令自动生成私有
          server/client/导入URI；界面只显示计划。常驻运行、凭据保存、核心切换、备份/回滚与证书自动续期还需接线。
        </p>
      </div>
    </section>
  );
}
