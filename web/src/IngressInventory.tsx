import { useEffect, useState } from 'react';
import { CircleNotch, Info } from '@phosphor-icons/react';
import { api } from './api';

type Source = { id: string; core: string; path: string; panel_managed: boolean };
type Inbound = {
  listener: string;
  id: string;
  tag: string;
  protocol: string;
  listen: string;
  port: number;
  transport: string;
  security: string;
  clients: number;
  vision: boolean;
  candidate: boolean;
  reason: string;
};
type Report = {
  source: Source;
  sha256: string;
  inbounds: Inbound[];
  read_only: boolean;
  core_validated: boolean;
  applied: boolean;
  warnings: string[];
};
export function IngressInventory() {
  const [sources, setSources] = useState<Source[]>([]);
  const [source, setSource] = useState('x-ui');
  const [confirm, setConfirm] = useState(false);
  const [report, setReport] = useState<Report | null>(null);
  const [selected, setSelected] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    let active = true;
    api<Source[]>('/host/ingress/sources')
      .then((value) => {
        if (active) setSources(value);
      })
      .catch((e) => {
        if (active) setError((e as Error).message);
      });
    return () => {
      active = false;
    };
  }, []);
  async function inspect() {
    setBusy(true);
    setError('');
    setReport(null);
    setSelected('');
    try {
      setReport(
        await api<Report>('/host/ingress/inspect', 'POST', { source, confirm_read: confirm }),
      );
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="panel ingress-inventory" aria-labelledby="ingress-inventory-title">
      <div className="section-heading">
        <div>
          <h2 id="ingress-inventory-title">已有入口配置</h2>
          <p>识别协议与配置端口，选择复用候选；本阶段不应用或重启。</p>
        </div>
        <Info size={21} />
      </div>
      <div className="ingress-content">
        <label>
          固定配置来源
          <select
            value={source}
            disabled={busy}
            onChange={(e) => {
              setSource(e.target.value);
              setReport(null);
              setConfirm(false);
            }}
          >
            {sources.map((item) => (
              <option key={item.id} value={item.id}>
                {item.core} · {item.path}
                {item.panel_managed ? '（面板生成）' : ''}
              </option>
            ))}
          </select>
        </label>
        <label className="confirm-check">
          <input
            type="checkbox"
            checked={confirm}
            disabled={busy}
            onChange={(e) => setConfirm(e.target.checked)}
          />
          我同意读取所选核心配置以提取入口摘要；不回显客户端 UUID、密码和私钥，不修改原配置。
        </label>
        <button
          className="button secondary"
          disabled={!confirm || busy || sources.length === 0}
          onClick={() => void inspect()}
        >
          {busy && <CircleNotch className="spin" size={17} />}
          {busy ? '正在解析入口' : '读取入口摘要'}
        </button>
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        {report && (
          <div aria-live="polite">
            <p className="muted">
              配置 SHA256：<code>{report.sha256}</code> · 尚未核心校验 · 未应用
            </p>
            {report.inbounds.length === 0 ? (
              <p>该配置没有入口。</p>
            ) : (
              <div className="checks">
                {report.inbounds.map((inbound) => (
                  <label className="ingress-entry" key={inbound.id}>
                    <input
                      type="radio"
                      name="reuse-inbound"
                      value={inbound.id}
                      checked={selected === inbound.id}
                      disabled={!inbound.candidate}
                      onChange={() => setSelected(inbound.id)}
                    />
                    <div>
                      <strong>
                        {inbound.tag || '无唯一标签'} · {inbound.protocol}
                      </strong>
                      <p>
                        {inbound.listen}:{inbound.port} · {inbound.transport} + {inbound.security}
                        {inbound.vision ? ' + Vision' : ''} · 客户端 {inbound.clients} ·{' '}
                        {inbound.listener === 'matching_address_port'
                          ? '观察到同地址/端口监听（归属未核验）'
                          : inbound.listener === 'not_observed'
                            ? '未观察到匹配监听'
                            : '监听未知'}
                      </p>
                      <p className="muted">{inbound.reason}</p>
                    </div>
                    <span className={`pill ${inbound.candidate ? 'teal' : 'amber'}`}>
                      {inbound.candidate ? '路由候选' : '需处理'}
                    </span>
                  </label>
                ))}
              </div>
            )}
            {selected && (
              <p className="callout">
                <Info size={18} />
                已选择复用候选。后续仅把此入口 TCP 送入 Miku 出口，UDP
                阻断；当前未保存绑定或接管。CLI 可生成私有候选，仍需真实核心校验与备份应用流程。
              </p>
            )}
            <ul className="inventory-warnings">
              {report.warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          </div>
        )}
        <p className="muted">
          配置权限不足时不会擅自 chmod
          或复制密钥到浏览器。面板生成文件需要持久化适配，不能只改临时文件。
        </p>
      </div>
    </section>
  );
}
