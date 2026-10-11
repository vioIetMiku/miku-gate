import { useEffect, useState } from 'react';
import { api, type Node, type Exit, timestamp } from './api';
type Policy = {
  enabled: boolean;
  resume_ingress: boolean;
  allow_legacy: boolean;
  node_ids: string[];
};
type State = {
  policy: Policy;
  status: {
    enabled: boolean;
    phase: string;
    attempts: number;
    next_attempt?: string;
    reason: string;
    ingress_reason?: string;
    current_candidate?: string;
  };
};
export function RecoverySettings({ exitID }: { exitID: string }) {
  const [value, setValue] = useState<State | null>(null);
  const [enabled, setEnabled] = useState(false);
  const [resume, setResume] = useState(false);
  const [legacy, setLegacy] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [nodes, setNodes] = useState<Node[]>([]);
  const [country, setCountry] = useState('');
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    let alive = true;
    Promise.all([
      api<State>(`/exits/${exitID}/recovery`),
      api<Exit[]>('/exits'),
      api<Node[]>('/nodes'),
    ])
      .then(([v, exits, n]) => {
        if (!alive) return;
        setValue(v);
        setEnabled(v.policy.enabled);
        setResume(v.policy.resume_ingress);
        setLegacy(v.policy.allow_legacy);
        setSelected(v.policy.node_ids);
        setCountry(exits.find((e) => e.id === exitID)?.country || '');
        setNodes(n);
      })
      .catch((e) => {
        if (alive) setError((e as Error).message);
      });
    const timer = window.setInterval(() => {
      if (!document.hidden)
        api<State>(`/exits/${exitID}/recovery`)
          .then((v) => {
            if (alive) setValue(v);
          })
          .catch(() => {});
    }, 10000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [exitID]);
  async function save() {
    setBusy(true);
    setError('');
    try {
      await api(`/exits/${exitID}/recovery`, 'PUT', {
        enabled,
        resume_ingress: resume,
        allow_legacy: legacy,
        node_ids: selected,
        confirm,
      });
      setValue(await api<State>(`/exits/${exitID}/recovery`));
      setConfirm(false);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="runtime-card" aria-label="自动恢复与同国家换线">
      <h3>自动恢复 / 同国家换线</h3>
      <p className="field-note">
        默认关闭。明确开启后会在故障和控制服务重启后尝试恢复；健康时不检测候选，失败先关闭转发，逐个候选重新校验证书/指纹，30秒起指数退避至15分钟，不直连。用户停止出口会持久关闭自动恢复。
      </p>
      <label className="confirm-check">
        <input
          type="checkbox"
          checked={enabled}
          disabled={busy}
          onChange={(e) => {
            setEnabled(e.target.checked);
            setConfirm(false);
          }}
        />
        启用此出口自动恢复
      </label>
      <label className="confirm-check">
        <input
          type="checkbox"
          checked={resume}
          disabled={busy}
          onChange={(e) => {
            setResume(e.target.checked);
            setConfirm(false);
          }}
        />
        恢复Miku受管加密入口（保留UUID/密码，重新验证REALITY目标）
      </label>
      <label className="confirm-check">
        <input
          type="checkbox"
          checked={legacy}
          disabled={busy}
          onChange={(e) => {
            setLegacy(e.target.checked);
            setConfirm(false);
          }}
        />
        候选允许明确VPN Gate旧数据算法兼容（不放宽TLS身份）
      </label>
      <label>
        可选同国家VPN备用节点（最多3个，留空仅重连当前配置）
        <select
          multiple
          value={selected}
          disabled={busy}
          onChange={(e) => {
            setSelected(Array.from(e.target.selectedOptions, (o) => o.value).slice(0, 3));
            setConfirm(false);
          }}
        >
          {nodes
            .filter((n) => n.country === country)
            .map((n) => (
              <option key={n.id} value={n.id}>
                {n.hostname} · {n.ip} · {n.country}
              </option>
            ))}
        </select>
      </label>
      <p className="field-note">
        HTTP/SOCKS自备上游只重连原配置，不混用VPN备用；无证据的严格住宅策略不会放行。候选过期/全部失败时继续停止并退避，凭据和监听端口不旋转。
      </p>
      <label className="confirm-check">
        <input
          type="checkbox"
          checked={confirm}
          disabled={busy}
          onChange={(e) => setConfirm(e.target.checked)}
        />
        确认按上述国家/兼容政策自动联网，允许恢复时短暂断线
      </label>
      <button
        type="button"
        className="button"
        disabled={busy || !confirm}
        onClick={() => void save()}
      >
        保存自动恢复设置
      </button>
      {value && (
        <p role="status">
          {value.status.reason}
          {value.status.ingress_reason ? ` · ${value.status.ingress_reason}` : ''} ·{' '}
          {value.status.phase} · 连续失败 {value.status.attempts}
          {value.status.current_candidate ? ` · 当前候选 ${value.status.current_candidate}` : ''}
          {value.status.next_attempt && !value.status.next_attempt.startsWith('0001-')
            ? ` · 下次 ${timestamp(value.status.next_attempt)}`
            : ''}
        </p>
      )}
      {error && (
        <div className="inline-error" role="alert">
          {error}
        </div>
      )}
    </section>
  );
}
