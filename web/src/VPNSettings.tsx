import { useEffect, useState, type FormEvent } from 'react';
import { CircleNotch, Info, ShieldCheck, ShieldWarning, Trash } from '@phosphor-icons/react';
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
  type NodeIdentity,
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
  const [manual, setManual] = useState(false);
  const [identity, setIdentity] = useState<NodeIdentity | null>(null);
  const [legacy, setLegacy] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [check, setCheck] = useState<ProfileCheck | null>(null);
  const [loading, setLoading] = useState(true);
  const [checking, setChecking] = useState(false);
  const [verifying, setVerifying] = useState(false);
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
          setManual(!value.peer_fingerprint);
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
    setIdentity(null);
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
    setManual(false);
    setIdentity(null);
    setError('');
  }
  async function verify() {
    setVerifying(true);
    setError('');
    setConfirm(false);
    try {
      const value = await api<NodeIdentity>(
        `/nodes/${encodeURIComponent(nodeID)}/identity`,
        'POST',
      );
      setIdentity(value);
      setPeerName(value.peer_name);
    } catch (err) {
      setIdentity(null);
      setError((err as Error).message);
    } finally {
      setVerifying(false);
    }
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (locked || busy || verifying) return;
    setBusy(true);
    setError('');
    try {
      await api(`/exits/${exit.id}/vpn`, 'PUT', {
        node_id: nodeID,
        peer_name: manual ? peerName : '',
        allow_legacy: legacy,
        confirm,
        use_verified_identity: !manual,
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
        if (!busy && !verifying) close();
      }}
    >
      <form className="modal-form upstream-form" onSubmit={save}>
        <div className="notice">
          <Info size={20} />
          <p>
            保存固定的官方配置快照。先验证官方域名的TLS身份并绑定证书指纹，再明确启动隧道；不会因为列表可解析就开放代理，也不修改宿主路由。
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
              。独立执行器需安装/管理者授权，网页不会自行获取root权限。
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
                  <dt>证书名称</dt>
                  <dd>{binding.peer_name}</dd>
                </div>
                <div>
                  <dt>身份绑定</dt>
                  <dd>
                    {binding.peer_fingerprint
                      ? '已保存精确证书指纹'
                      : '手动名称（无自动TLS检查记录）'}
                  </dd>
                </div>
                <div>
                  <dt>保存时间</dt>
                  <dd>{timestamp(binding.updated_at)}</dd>
                </div>
              </dl>
            )}
            <fieldset
              className="upstream-fields"
              disabled={locked || busy || verifying || !binding}
            >
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
                <p className="field-note">先在“节点库”同步官方列表；同步本身不会连接VPN。</p>
              )}
              {checking && <p role="status">检查静态配置…</p>}
              {check && <p className="field-note">{check.message}</p>}
              <label className="option-check">
                <input
                  type="checkbox"
                  checked={manual}
                  onChange={(e) => {
                    setManual(e.target.checked);
                    setConfirm(false);
                  }}
                />
                <span>高级：手动指定预期证书名称（只用于已从独立可信来源确认名称的情况）</span>
              </label>
              {manual ? (
                <>
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
                      placeholder="从运营方或可信来源确认的证书CN"
                    />
                  </label>
                  <div className="notice">
                    <ShieldWarning size={19} />
                    <p>
                      列表主机名不一定等于证书CN。手动模式不会自动保存证书指纹；不要根据一次连接错误盲目接受新名字，不提供关闭TLS验证的选项。
                    </p>
                  </div>
                </>
              ) : (
                <>
                  <p className="field-note">
                    点击后会向此节点进行一次标准TLS检查：验证系统CA和官方 vpn…opengw.net
                    域名，保存对应证书CN与SHA-256。TCP使用公布端口；UDP节点检查443，不扫描其他端口。此步不发送VPN凭据。
                  </p>
                  <button
                    type="button"
                    className="button"
                    disabled={!nodeID || checking || check?.mode === 'unsupported'}
                    onClick={() => void verify()}
                  >
                    {verifying ? (
                      <CircleNotch className="spin" size={17} />
                    ) : (
                      <ShieldCheck size={17} />
                    )}
                    验证官方TLS身份
                  </button>
                  {identity && (
                    <div className="runtime-card">
                      <div className="runtime-heading">
                        <h3>身份验证通过</h3>
                        <span className="pill teal">{identity.tls_version}</span>
                      </div>
                      <dl className="credential-list">
                        <div>
                          <dt>验证域名</dt>
                          <dd>{identity.tls_host}</dd>
                        </div>
                        <div>
                          <dt>证书名称</dt>
                          <dd>{identity.peer_name}</dd>
                        </div>
                        <div>
                          <dt>证书指纹</dt>
                          <dd>
                            <code title={identity.peer_fingerprint}>
                              {identity.peer_fingerprint.slice(0, 20)}…
                            </code>
                          </dd>
                        </div>
                        <div>
                          <dt>检查时间</dt>
                          <dd>{timestamp(identity.checked_at)}</dd>
                        </div>
                      </dl>
                      <p className="field-note">
                        本次记录5分钟内可用于保存，节点/配置变化后需重检。{identity.message}
                      </p>
                    </div>
                  )}
                </>
              )}
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
                  Gate旧数据通道（AES-128-CBC/HMAC-SHA1）及精确固定的公开共享客户端身份；仍验证服务器TLS、名称、指纹和有效期。
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
                  我同意按上述身份与兼容政策使用此节点，并替换本出口原有代理上游。保存不自动启动，也不保证节点为住宅IP或运营者可信。
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
            <p>移除后不能再按此快照启动VPN，入口端口和凭据保留。</p>
            <button
              className="button danger"
              type="button"
              disabled={busy || locked || verifying}
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
              disabled={busy || locked || verifying}
              onClick={() => setRemove(!remove)}
            >
              <Trash size={16} />
              移除VPN
            </button>
          )}
          <button type="button" className="button" onClick={close} disabled={busy || verifying}>
            关闭
          </button>
          <button
            className="button primary"
            disabled={
              busy ||
              verifying ||
              locked ||
              loading ||
              checking ||
              !nodeID ||
              !confirm ||
              (!manual && !identity) ||
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
