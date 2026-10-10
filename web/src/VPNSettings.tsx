import { useEffect, useState, type FormEvent } from 'react';
import { CircleNotch, Info, ShieldWarning, Trash } from '@phosphor-icons/react';
import { Modal } from './Modal';
import {
  api,
  hasRuntime,
  timestamp,
  type Exit,
  type Node,
  type ProfileCheck,
  type VPNBinding,
  type VPNExecutor,
} from './api';

export function VPNSettings({
  exit,
  nodes,
  close,
  saved,
}: {
  exit: Exit;
  nodes: Node[];
  close: () => void;
  saved: () => Promise<void>;
}) {
  const [binding, setBinding] = useState<VPNBinding | null>(null);
  const [executor, setExecutor] = useState<VPNExecutor | null>(null);
  const [nodeID, setNodeID] = useState('');
  const [peerName, setPeerName] = useState('');
  const [legacy, setLegacy] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [check, setCheck] = useState<ProfileCheck | null>(null);
  const [loading, setLoading] = useState(true);
  const [checking, setChecking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [remove, setRemove] = useState(false);
  const locked = hasRuntime(exit.status);
  useEffect(() => {
    let active = true;
    Promise.all([api<VPNBinding>(`/exits/${exit.id}/vpn`), api<VPNExecutor>('/vpn-executor')])
      .then(([value, status]) => {
        if (!active) return;
        setBinding(value);
        setExecutor(status);
        if (value.configured) {
          setNodeID(value.node_id);
          setPeerName(value.peer_name);
          setLegacy(value.allow_legacy);
        }
      })
      .catch((err) => {
        if (active) setError((err as Error).message);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [exit.id]);
  useEffect(() => {
    let active = true;
    setCheck(null);
    if (!nodeID)
      return () => {
        active = false;
      };
    setChecking(true);
    api<ProfileCheck>(`/nodes/${encodeURIComponent(nodeID)}/profile`)
      .then((value) => {
        if (active) setCheck(value);
      })
      .catch((err) => {
        if (active) setError((err as Error).message);
      })
      .finally(() => {
        if (active) setChecking(false);
      });
    return () => {
      active = false;
    };
  }, [nodeID]);
  function selectNode(id: string) {
    setNodeID(id);
    setPeerName(nodes.find((n) => n.id === id)?.hostname ?? '');
    setConfirm(false);
    setLegacy(false);
    setError('');
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (locked || busy) return;
    setBusy(true);
    setError('');
    try {
      await api(`/exits/${exit.id}/vpn`, 'PUT', {
        node_id: nodeID,
        peer_name: peerName,
        allow_legacy: legacy,
        confirm,
      });
      await saved();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function removeBinding() {
    setBusy(true);
    setError('');
    try {
      await api(`/exits/${exit.id}/vpn`, 'DELETE');
      await saved();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title={`OpenVPN 节点 · ${exit.name}`}
      close={() => {
        if (!busy) close();
      }}
    >
      <form className="modal-form upstream-form" onSubmit={save}>
        <div className="notice">
          <Info size={20} />
          <p>
            保存的是固定的官方配置快照，不随节点库刷新自动换线。只有明确启动、验证TLS身份、TUN/DNS和实际出口后才开放入口；不修改宿主路由。
          </p>
        </div>
        {loading ? (
          <p role="status">
            <CircleNotch className="spin" size={18} />
            读取VPN绑定与执行器…
          </p>
        ) : (
          <>
            <p className="field-note">
              {executor?.message ?? '执行器状态未知'}
              。安装/管理者需显式授权独立执行器；界面不能自行取得root权限。
            </p>
            {locked && (
              <div className="inline-error" role="status">
                出口正在运行或停止中，请先停止后再选择节点。
              </div>
            )}
            {binding?.configured && (
              <dl className="credential-list">
                <div>
                  <dt>当前快照</dt>
                  <dd>{binding.node_hostname}</dd>
                </div>
                <div>
                  <dt>预期证书名称</dt>
                  <dd>{binding.peer_name}</dd>
                </div>
                <div>
                  <dt>保存时间</dt>
                  <dd>{timestamp(binding.updated_at)}</dd>
                </div>
                <div>
                  <dt>配置指纹</dt>
                  <dd>
                    <code>{binding.profile_sha256.slice(0, 16)}…</code>
                  </dd>
                </div>
              </dl>
            )}
            <fieldset className="upstream-fields" disabled={locked || busy || !binding}>
              <label>
                官方候选节点
                <select value={nodeID} onChange={(e) => selectNode(e.target.value)} required>
                  <option value="">请选择已同步的节点</option>
                  {binding?.configured && !nodes.some((n) => n.id === binding.node_id) && (
                    <option value={binding.node_id}>
                      {binding.node_hostname}（已不在当前缓存）
                    </option>
                  )}
                  {nodes.map((node) => (
                    <option key={node.id} value={node.id}>
                      {node.country} · {node.hostname} · {node.ip}
                    </option>
                  ))}
                </select>
              </label>
              {nodes.length === 0 && (
                <p className="field-note">
                  当前节点库为空。请先在“节点库”同步官方列表；同步不等于授权连接。
                </p>
              )}
              <label>
                预期服务器证书名称
                <input
                  value={peerName}
                  onChange={(e) => {
                    setPeerName(e.target.value);
                    setConfirm(false);
                  }}
                  required
                  maxLength={128}
                  autoComplete="off"
                  placeholder="从运营方或可信来源确认的名称"
                />
              </label>
              <p className="field-note">
                选择时预填的是官方列表主机名，未必等于证书名称。必须核实来源；不要因连接报错就接受日志中任意新名字。只信CA而不核对名称并不足够。
              </p>
              {checking && <p role="status">检查静态配置…</p>}
              {check && <p className="field-note">{check.message}</p>}
              <label className="option-check">
                <input
                  type="checkbox"
                  checked={legacy}
                  onChange={(e) => {
                    setLegacy(e.target.checked);
                    setConfirm(false);
                  }}
                />
                <span>
                  明确允许VPN
                  Gate旧数据通道（AES-128-CBC/HMAC-SHA1）及精确固定的公开共享客户端身份；仍不跳过服务器TLS、名称或有效期检查。
                </span>
              </label>
              <label className="option-check">
                <input
                  type="checkbox"
                  checked={confirm}
                  onChange={(e) => setConfirm(e.target.checked)}
                  required
                />
                <span>
                  我已核实所填服务器名称来源，并同意此选择会替换本出口现有代理上游。保存不自动启动，也不意味着节点是住宅IP或值得信任。
                </span>
              </label>
            </fieldset>
          </>
        )}
        {error && (
          <div className="inline-error" role="alert">
            {error}
          </div>
        )}
        {remove && (
          <div className="notice">
            <ShieldWarning size={20} />
            <p>移除后无法按此快照启动VPN，入口端口和凭据保留。</p>
            <button
              className="button danger"
              type="button"
              disabled={busy || locked}
              onClick={() => void removeBinding()}
            >
              确认移除VPN绑定
            </button>
          </div>
        )}
        <div className="modal-actions">
          {binding?.configured && (
            <button
              className="button danger"
              type="button"
              disabled={busy || locked}
              onClick={() => setRemove(!remove)}
            >
              <Trash size={16} />
              移除VPN
            </button>
          )}
          <button type="button" className="button" onClick={close} disabled={busy}>
            关闭
          </button>
          <button
            className="button primary"
            disabled={
              busy ||
              locked ||
              loading ||
              checking ||
              !nodeID ||
              !confirm ||
              check?.mode === 'unsupported' ||
              (!!check?.legacy_candidate && !legacy)
            }
          >
            {busy ? <CircleNotch className="spin" size={17} /> : '保存VPN绑定'}
          </button>
        </div>
      </form>
    </Modal>
  );
}
