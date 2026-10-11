import { useEffect, useState } from 'react';
import { CircleNotch, Info } from '@phosphor-icons/react';
import { api, type Exit } from './api';

type Endpoint = { port: number; base: string; mode: string };
type Preview = {
  preview_id: string;
  template_sha256: string;
  rendered_sha256: string;
  report: {
    inbounds: {
      id: string;
      tag: string;
      port: number;
      protocol: string;
      candidate: boolean;
      reason: string;
    }[];
  };
};
type RoutePlan = {
  candidate_sha256: string;
  inbound_tag: string;
  proxy_address: string;
  proxy_port: number;
  udp_blocked: boolean;
  other_inbounds_preserved: boolean;
};
type Backup = {
  backup_id: string;
  endpoint: Endpoint;
  exit_id: string;
  inbound_tag: string;
  recovery_required?: boolean;
};
export function PanelReuse() {
  const [port, setPort] = useState('2053');
  const [base, setBase] = useState('/');
  const [mode, setMode] = useState('current');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [twoFactor, setTwoFactor] = useState('');
  const [consent, setConsent] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [preview, setPreview] = useState<Preview | null>(null);
  const [inbound, setInbound] = useState('');
  const [exits, setExits] = useState<Exit[]>([]);
  const [exitID, setExitID] = useState('');
  const [exitPassword, setExitPassword] = useState('');
  const [backups, setBackups] = useState<Backup[]>([]);
  const [routePlan, setRoutePlan] = useState<RoutePlan | null>(null);
  const [forgetID, setForgetID] = useState<string | null>(null);
  useEffect(() => {
    setRoutePlan(null);
    setConfirm(false);
  }, [inbound, exitID, exitPassword]);
  useEffect(() => {
    let alive = true;
    api<Exit[]>('/exits')
      .then((value) => {
        if (alive) setExits(value);
      })
      .catch(() => {});
    api<Backup[]>('/host/panel/backups')
      .then((value) => {
        if (alive) setBackups(value);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);
  async function inspect() {
    setBusy(true);
    setError('');
    setNotice('');
    setPreview(null);
    setInbound('');
    setConfirm(false);
    try {
      const value = await api<Preview>('/host/panel/inspect', 'POST', {
        endpoint: { port: Number(port), base, mode },
        username,
        password,
        two_factor: twoFactor,
        confirm: consent,
      });
      setPreview(value);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setPassword('');
      setTwoFactor('');
      setBusy(false);
    }
  }
  async function plan() {
    if (!preview) return;
    setBusy(true);
    setError('');
    try {
      const result = await api<{ plan: RoutePlan }>('/host/panel/plan', 'POST', {
        preview_id: preview.preview_id,
        template_sha256: preview.template_sha256,
        rendered_sha256: preview.rendered_sha256,
        inbound_id: inbound,
        exit_id: exitID,
        exit_password: exitPassword,
        confirm: true,
      });
      setRoutePlan(result.plan);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function forget() {
    if (!forgetID) return;
    setBusy(true);
    setError('');
    try {
      await api(`/host/panel/backups/${encodeURIComponent(forgetID)}`, 'DELETE', { confirm: true });
      setBackups(await api<Backup[]>('/host/panel/backups'));
      setForgetID(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function apply(backup?: Backup) {
    if (!preview) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const value = await api<{ backup_id: string }>(
        '/host/panel/' + (backup ? 'rollback' : 'apply'),
        'POST',
        {
          preview_id: preview.preview_id,
          template_sha256: preview.template_sha256,
          rendered_sha256: preview.rendered_sha256,
          ...(backup
            ? { backup_id: backup.backup_id }
            : {
                inbound_id: inbound,
                exit_id: exitID,
                exit_password: exitPassword,
                candidate_sha256: routePlan?.candidate_sha256,
              }),
          confirm,
        },
      );
      setNotice(
        `已回读验证持久模板${backup ? '恢复' : '应用'}，原入口身份保留。加密备份：${value.backup_id}。仍需实际客户端连通性检查，不宣称业务路径已验证。`,
      );
      setPreview(null);
      setConfirm(false);
      setBackups(await api<Backup[]>('/host/panel/backups'));
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setExitPassword('');
      setBusy(false);
    }
  }
  return (
    <section className="panel ingress-inventory" aria-labelledby="panel-reuse-title">
      <div className="section-heading">
        <div>
          <h2 id="panel-reuse-title">复用本机 3x-ui 已有节点</h2>
          <p>通过面板持久 API 保存路由，不修改会被重写的生成配置。</p>
        </div>
        <Info size={21} />
      </div>
      <div className="ingress-content">
        <div className="notice">
          <Info size={20} />
          <p>
            只连接127.0.0.1。登录密码不保存，面板会话/预览5分钟过期。应用或回滚会重启面板核心、短暂中断已有连接；先启动健康Miku出口，不接受配置地址冒充可用状态。最多保留8份加密快照，DB/key必须配套保管。
          </p>
        </div>
        <div className="form-row">
          <label>
            面板本机端口
            <input
              inputMode="numeric"
              value={port}
              disabled={busy || !!preview}
              onChange={(e) => setPort(e.target.value)}
            />
          </label>
          <label>
            面板基础路径
            <input
              value={base}
              disabled={busy || !!preview}
              onChange={(e) => setBase(e.target.value)}
              placeholder="/secret-base/"
            />
          </label>
        </div>
        <label>
          明确面板接口版本
          <select
            value={mode}
            disabled={busy || !!preview}
            onChange={(e) => setMode(e.target.value)}
          >
            <option value="current">新版 panel/api/xray + CSRF</option>
            <option value="legacy">旧版 panel/xray（明确选择，不自动遍历）</option>
          </select>
        </label>
        {!preview && (
          <>
            <div className="form-row">
              <label>
                面板用户名
                <input
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="off"
                  maxLength={255}
                />
              </label>
              <label>
                面板密码
                <input
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="new-password"
                  maxLength={512}
                />
              </label>
            </div>
            <label>
              面板两步验证码（如启用）
              <input
                value={twoFactor}
                onChange={(e) => setTwoFactor(e.target.value)}
                autoComplete="off"
                maxLength={32}
              />
            </label>
          </>
        )}
        <label className="confirm-check">
          <input type="checkbox" checked={consent} onChange={(e) => setConsent(e.target.checked)} />
          确认读取本机面板持久模板与节点配置，发送上述登录凭据，仅返回脱敏摘要
        </label>
        <div className="modal-actions">
          <button
            className="button"
            disabled={busy || !consent || !username || !password}
            onClick={() => void inspect()}
          >
            {busy ? <CircleNotch className="spin" size={16} /> : null}登录并检测持久入口
          </button>
          {preview && (
            <button
              className="button"
              disabled={busy}
              onClick={() => {
                setPreview(null);
                setConfirm(false);
              }}
            >
              重新登录检测
            </button>
          )}
        </div>
        {preview && (
          <>
            <label>
              选择已有入口
              <select value={inbound} onChange={(e) => setInbound(e.target.value)}>
                <option value="">请选择</option>
                {preview.report.inbounds.map((item) => (
                  <option key={item.id} value={item.id} disabled={!item.candidate}>
                    {item.tag} · {item.protocol} · {item.port}
                    {!item.candidate ? ' · 不支持受控复用' : ''}
                  </option>
                ))}
              </select>
            </label>
            <label>
              绑定 Miku 逻辑出口
              <select value={exitID} onChange={(e) => setExitID(e.target.value)}>
                <option value="">请选择已启动健康出口</option>
                {exits.map((exit) => (
                  <option key={exit.id} value={exit.id}>
                    {exit.name} · {exit.status}
                  </option>
                ))}
              </select>
            </label>
            <label>
              所选逻辑出口的代理密码
              <input
                type="password"
                value={exitPassword}
                onChange={(e) => setExitPassword(e.target.value)}
                autoComplete="new-password"
                maxLength={255}
              />
            </label>
            <button
              className="button"
              disabled={busy || !inbound || !exitID || !exitPassword}
              onClick={() => void plan()}
            >
              预览所选持久路由
            </button>
            {routePlan && (
              <div className="notice" role="status">
                入口 {routePlan.inbound_tag} 的TCP → {routePlan.proxy_address}:
                {routePlan.proxy_port}；UDP阻断：{routePlan.udp_blocked ? '是' : '否'}
                ；其他入口保留：{routePlan.other_inbounds_preserved ? '是' : '否'}
                。尚未应用或验证业务。
              </div>
            )}
            <p className="field-note">
              模板摘要 {preview.template_sha256}
              。预览最多5分钟；双摘要变化时拒绝写入，客户端身份与其他节点不改。
            </p>
            <label className="confirm-check">
              <input
                type="checkbox"
                checked={confirm}
                onChange={(e) => setConfirm(e.target.checked)}
              />
              确认创建私有加密备份并应用/回滚持久路由，允许面板核心重启
            </label>
            <button
              className="button primary"
              disabled={busy || !confirm || !inbound || !exitID || !exitPassword || !routePlan}
              onClick={() => void apply()}
            >
              备份并应用持久路由
            </button>
            {backups
              .filter(
                (item) =>
                  item.endpoint.port === Number(port) &&
                  item.endpoint.base === base &&
                  item.endpoint.mode === mode,
              )
              .map((item) => (
                <div className="runtime-actions" key={item.backup_id}>
                  <span>
                    私有快照 {item.backup_id} · {item.inbound_tag || '回滚前状态'}
                    {item.recovery_required ? ' · 未完成事务，请先恢复' : ''}
                  </span>
                  <button
                    className="button"
                    disabled={busy || !confirm}
                    onClick={() => void apply(item)}
                  >
                    恢复此快照
                  </button>
                </div>
              ))}
          </>
        )}
        {backups.length > 0 && (
          <details>
            <summary>私有恢复快照管理（{backups.length}/8）</summary>
            {backups.map((item) => (
              <div className="runtime-actions" key={item.backup_id}>
                <span>
                  {item.backup_id} · {item.inbound_tag || '回滚前状态'}
                  {item.recovery_required ? ' · 未完成事务，请先恢复' : ''}
                </span>
                <button
                  className="text-button"
                  disabled={busy}
                  onClick={() => setForgetID(item.backup_id)}
                >
                  删除此快照
                </button>
              </div>
            ))}
          </details>
        )}
        {forgetID && (
          <div className="notice">
            <p>
              删除恢复快照 {forgetID}{' '}
              后不可再用它回滚。不会撤销当前面板路由；请确认已有其他配套备份。
            </p>
            <button className="button" disabled={busy} onClick={() => setForgetID(null)}>
              取消
            </button>
            <button className="button" disabled={busy} onClick={() => void forget()}>
              确认删除恢复快照
            </button>
          </div>
        )}
        {error && (
          <div className="inline-error" role="alert">
            {error}
          </div>
        )}
        {notice && (
          <div className="notice" role="status">
            {notice}
          </div>
        )}
        <p className="field-note">
          真实面板版本兼容和完整客户端数据路径仍需实际验证；此处回读配置不是宣称已连接VPN。面板只HTTPS监听/非标准路径当前明确不可用，不忽略证书或扫描端口。
        </p>
      </div>
    </section>
  );
}
