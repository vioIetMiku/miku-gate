import { useEffect, useState } from 'react';
import { Info } from '@phosphor-icons/react';
import { api, timestamp } from './api';
type Access = {
  mode: string;
  origin: string;
  tls_active: boolean;
  explicit_origin_configured: boolean;
  certificate_status: string;
  certificate?: {
    leaf_sha256: string;
    not_after: string;
    system_trusted: boolean;
    bootstrap: boolean;
  };
};
export function ManagementAccess() {
  const [access, setAccess] = useState<Access | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let active = true;
    api<Access>('/management')
      .then((value) => {
        if (active) setAccess(value);
      })
      .catch((e) => {
        if (active) setError((e as Error).message);
      });
    return () => {
      active = false;
    };
  }, []);
  return (
    <section className="panel management-access" aria-labelledby="management-access-title">
      <div className="section-heading">
        <div>
          <h2 id="management-access-title">管理访问与证书</h2>
          <p>显示当前服务的真实访问模式；客户端加密入口不等于管理端HTTPS。</p>
        </div>
        <Info size={20} />
      </div>
      <div className="management-access-content">
        {error && <p role="alert">{error}</p>}
        {!access && !error && <p>正在读取当前管理传输…</p>}
        {access && (
          <>
            <dl className="settings-list">
              <div>
                <dt>管理地址</dt>
                <dd>{access.origin}</dd>
              </div>
              <div>
                <dt>连接加密</dt>
                <dd>{access.tls_active ? '当前连接使用TLS' : '仅本机回环HTTP，不允许公开明文'}</dd>
              </div>
              <div>
                <dt>授权范围</dt>
                <dd>
                  {access.explicit_origin_configured
                    ? '仅明确配置的Host / HTTPS Origin'
                    : '仅回环Host'}
                </dd>
              </div>
            </dl>
            {access.certificate ? (
              <>
                <p>
                  叶证书 SHA256：<code>{access.certificate.leaf_sha256}</code>
                </p>
                <p>有效期至：{timestamp(access.certificate.not_after)}</p>
                <p className={access.certificate.bootstrap ? 'amber-text' : 'muted'}>
                  {access.certificate.bootstrap
                    ? 'IP自签引导身份：不是公共CA信任。使用可信SSH/安装输出核对指纹或导入证书后输入密码，不能忽略证书警告。'
                    : '服务器已校验系统信任链和域名/IP；还需保持续期与安全权限。'}
                </p>
              </>
            ) : (
              <p className="muted">当前没有返回已验证证书摘要；不会把未知状态标成可信。</p>
            )}
            <p className="muted">
              不信任转发头替代TLS或改变管理地址；私钥不返回浏览器。域名与IP安装使用独立HTTPS端口，不抢原80/443服务。
            </p>
          </>
        )}
      </div>
    </section>
  );
}
