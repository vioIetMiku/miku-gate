import { useEffect, useState } from 'react';
import { CircleNotch, Info, ShieldWarning } from '@phosphor-icons/react';
import { Modal } from './Modal';
import { api, timestamp, type Node, type ProfileCheck } from './api';

export function NodeProfileView({ node, close }: { node: Node; close: () => void }) {
  const [result, setResult] = useState<ProfileCheck | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let active = true;
    api<ProfileCheck>(`/nodes/${encodeURIComponent(node.id)}/profile`)
      .then((value) => {
        if (active) setResult(value);
      })
      .catch((err) => {
        if (active) setError((err as Error).message);
      });
    return () => {
      active = false;
    };
  }, [node.id]);
  const title =
    result?.mode === 'strict'
      ? '严格格式可接受'
      : result?.mode === 'legacy-candidate'
        ? '需要 VPN Gate 兼容模式'
        : '配置不受支持';
  return (
    <Modal title={`配置检查 · ${node.hostname}`} close={close}>
      <div className="modal-form">
        <div className="notice">
          <Info size={20} />
          <p>
            仅分析缓存配置，未联系这个VPN节点。通过格式检查不等于连通、服务器可信或出口为住宅IP。
          </p>
        </div>
        {error && (
          <div className="inline-error" role="alert">
            {error}
          </div>
        )}
        {!result && !error && (
          <p role="status">
            <CircleNotch className="spin" size={18} />
            正在检查配置格式与证书政策…
          </p>
        )}
        {result && (
          <>
            <div className="runtime-heading">
              <h3>{title}</h3>
              <span className="pill">未测试网络</span>
            </div>
            <dl className="credential-list">
              <div>
                <dt>严格 AEAD 策略</dt>
                <dd>{result.strict_compatible ? '通过格式检查' : '未通过'}</dd>
              </div>
              <div>
                <dt>旧数据通道</dt>
                <dd>{result.legacy_data_channel ? 'AES-128-CBC / HMAC-SHA1' : '未启用'}</dd>
              </div>
              <div>
                <dt>共享客户端身份</dt>
                <dd>
                  {result.shared_client_identity
                    ? '精确指纹识别的VPN Gate公开身份'
                    : '没有使用特殊例外'}
                </dd>
              </div>
              <div>
                <dt>服务器 TLS 下限</dt>
                <dd>{result.tls_minimum} · 不跳过证书校验</dd>
              </div>
              <div>
                <dt>实际服务器身份</dt>
                <dd>未验证，连接前仍需可信名称绑定</dd>
              </div>
              <div>
                <dt>检查时间</dt>
                <dd>{timestamp(result.checked_at)}</dd>
              </div>
            </dl>
            <p className="field-note">{result.message}</p>
            {result.legacy_candidate && (
              <div className="notice">
                <ShieldWarning size={20} />
                <p>
                  后续连接需要明确同意兼容模式；这次查看没有授权或启动连接。只兼容旧数据通道和精确固定的公开客户端证书，不笼统接受任意SHA1服务器证书、脚本、插件或压缩。
                </p>
              </div>
            )}
          </>
        )}
        <div className="modal-actions">
          <button type="button" className="button" onClick={close}>
            关闭
          </button>
        </div>
      </div>
    </Modal>
  );
}
