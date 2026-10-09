import { useEffect, useRef, useState } from 'react';
import { ArrowClockwise, CircleNotch, ShieldWarning } from '@phosphor-icons/react';
import { api, type ExecutorStatus, type SandboxState } from './api';

export function SandboxRuntimeView({ exitID }: { exitID: string }) {
  const [executor, setExecutor] = useState<ExecutorStatus | null>(null);
  const [actual, setActual] = useState<SandboxState | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [confirmation, setConfirmation] = useState<'prepare' | 'cleanup' | null>(null);
  const generation = useRef(0);
  useEffect(
    () => () => {
      generation.current++;
    },
    [exitID],
  );
  const base = '/exits/' + encodeURIComponent(exitID) + '/sandbox';

  async function inspect() {
    const request = ++generation.current;
    setBusy(true);
    setError('');
    setConfirmation(null);
    setActual(null);
    try {
      const status = await api<ExecutorStatus>('/executor');
      if (request !== generation.current) return;
      setExecutor(status);
      if (status.reachable) {
        const state = await api<SandboxState>(base);
        if (request === generation.current) setActual(state);
      }
    } catch (cause) {
      if (request === generation.current)
        setError(cause instanceof Error ? cause.message : '隔离状态读取失败');
    } finally {
      if (request === generation.current) setBusy(false);
    }
  }
  async function apply(operation: 'prepare' | 'cleanup') {
    const request = ++generation.current;
    setBusy(true);
    setError('');
    setConfirmation(null);
    try {
      const state = await api<SandboxState>(base + '/' + operation, 'POST', { confirm: true });
      if (request === generation.current) setActual(state);
    } catch (cause) {
      if (request === generation.current) {
        setActual(null);
        setError(
          (cause instanceof Error ? cause.message : '操作未确认完成') +
            ' 请先重新检查实际状态，不要盲目重试。',
        );
      }
    } finally {
      if (request === generation.current) setBusy(false);
    }
  }
  return (
    <section className="network-plan" aria-label="实际隔离状态">
      <div className="network-plan-heading">
        <div>
          <h3>实际隔离状态</h3>
          <p className="muted">与上面的计划分开查询；即使已隔离，也不是 VPN 在线。</p>
        </div>
        <button className="button" type="button" disabled={busy} onClick={() => void inspect()}>
          {busy ? <CircleNotch size={17} className="spin" /> : <ArrowClockwise size={17} />}
          检查执行器
        </button>
      </div>
      {error && (
        <p role="alert" className="network-plan-error">
          {error}
        </p>
      )}
      {executor && <p className="muted">{executor.message}</p>}
      {actual && (
        <>
          <dl className="credential-list">
            <div>
              <dt>实际阶段</dt>
              <dd>
                <code>{actual.phase}</code>
              </dd>
            </div>
            <div>
              <dt>本次隔离核验</dt>
              <dd>{actual.quarantine_verified ? '已核验全阻断规则' : '未确认'}</dd>
            </div>
            <div>
              <dt>VPN / 代理</dt>
              <dd>仍未启用</dd>
            </div>
          </dl>
          <p className="muted">{actual.reason}</p>
          <div className="detail-actions">
            <button
              className="button"
              type="button"
              disabled={busy || actual.phase !== 'absent'}
              onClick={() => setConfirmation('prepare')}
            >
              创建隔离沙箱
            </button>
            <button
              className="button"
              type="button"
              disabled={busy || actual.phase === 'absent'}
              onClick={() => setConfirmation('cleanup')}
            >
              清理隔离沙箱
            </button>
          </div>
        </>
      )}
      {confirmation && (
        <div className="network-plan-content">
          <div className="notice">
            <ShieldWarning size={20} />
            <p>
              {confirmation === 'prepare'
                ? '将通过已授权的 root 执行器创建不联网的全阻断 namespace，不会连接 VPN。'
                : '仅回收能证明归属且未被占用的本出口沙箱；不会杀死进程或删除陌生资源。'}
            </p>
          </div>
          <div className="detail-actions">
            <button
              className="button"
              type="button"
              disabled={busy}
              onClick={() => setConfirmation(null)}
            >
              取消
            </button>
            <button
              className="button"
              type="button"
              disabled={busy}
              onClick={() => void apply(confirmation)}
            >
              确认{confirmation === 'prepare' ? '创建' : '清理'}隔离沙箱
            </button>
          </div>
        </div>
      )}
    </section>
  );
}
