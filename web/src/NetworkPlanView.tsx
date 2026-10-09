import { useEffect, useRef, useState } from 'react';
import { ArrowClockwise, CircleNotch, Info, ShieldWarning } from '@phosphor-icons/react';
import { api, type NetworkPlan } from './api';

export function NetworkPlanView({ exitID }: { exitID: string }) {
  const [plan, setPlan] = useState<NetworkPlan | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const generation = useRef(0);
  useEffect(() => {
    return () => {
      generation.current++;
    };
  }, [exitID]);

  async function load() {
    const request = ++generation.current;
    setBusy(true);
    setError('');
    try {
      const next = await api<NetworkPlan>('/exits/' + encodeURIComponent(exitID) + '/network-plan');
      if (request === generation.current) setPlan(next);
    } catch (cause) {
      if (request === generation.current)
        setError(cause instanceof Error ? cause.message : '计划加载失败');
    } finally {
      if (request === generation.current) setBusy(false);
    }
  }

  return (
    <section className="network-plan" aria-label="网络隔离计划">
      <div className="network-plan-heading">
        <div>
          <h3>网络隔离计划</h3>
          <p className="muted">只读预览，不执行系统命令，不建立连接。</p>
        </div>
        <button type="button" className="button" disabled={busy} onClick={() => void load()}>
          {busy ? <CircleNotch className="spin" size={17} /> : <ArrowClockwise size={17} />}
          {plan ? '刷新计划' : '预览隔离计划'}
        </button>
      </div>
      {busy && <p role="status">正在生成只读计划…</p>}
      {error && (
        <p className="network-plan-error" role="alert">
          {error}
        </p>
      )}
      {plan && (
        <div className="network-plan-content">
          <div className="notice">
            <ShieldWarning size={20} />
            <p>阶段：仅隔离沙箱。以下是计划，不是运行状态或防泄漏测试结果。</p>
          </div>
          <dl className="credential-list">
            <div>
              <dt>实例 ID</dt>
              <dd>
                <code>{plan.instance_id}</code>
              </dd>
            </div>
            <div>
              <dt>Namespace</dt>
              <dd>
                <code>{plan.namespace}</code>
              </dd>
            </div>
            <div>
              <dt>隔离规则表</dt>
              <dd>
                <code>{plan.nft_table}</code>
              </dd>
            </div>
            <div>
              <dt>宿主路由 / 防火墙</dt>
              <dd>
                {plan.host_routes_changed || plan.host_firewall_changed
                  ? '计划包含变更，请停止并核验'
                  : '均不修改'}
              </dd>
            </div>
            <div>
              <dt>VPN / 代理监听</dt>
              <dd>
                {plan.tunnel_enabled || plan.proxy_enabled
                  ? '计划包含启用动作，请核验'
                  : '均不启用'}
              </dd>
            </div>
          </dl>
          <ol className="network-plan-steps">
            {plan.changes.map((change) => (
              <li key={change.action}>{change.description}</li>
            ))}
          </ol>
          <details>
            <summary>查看拟安装的 nftables 规则</summary>
            <pre tabIndex={0} aria-label="nftables 隔离规则">
              <code>{plan.nft_ruleset}</code>
            </pre>
          </details>
          <div className="notice">
            <Info size={20} />
            <p>
              仅 Linux 管理员可通过独立 sandbox CLI 手动执行，prepare / cleanup 必须显式使用
              --apply。Web 控制台不能调用特权操作。
            </p>
          </div>
        </div>
      )}
    </section>
  );
}
