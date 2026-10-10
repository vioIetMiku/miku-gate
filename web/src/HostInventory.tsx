import { useEffect, useRef, useState } from 'react';
import { ArrowClockwise, CircleNotch, Info } from '@phosphor-icons/react';
import { api, timestamp } from './api';

type Evidence = { path: string; kind: string; state: string };
type Report = {
  checked_at: string;
  platform: string;
  supported: boolean;
  read_only: boolean;
  privileged: boolean;
  distribution: string;
  distribution_version: string;
  tun: string;
  systemd: string;
  dependencies: { name: string; purpose: string; package_hint: string; state: string }[];
  components: {
    id: string;
    name: string;
    state: string;
    version: string;
    reuse_ready: boolean;
    evidence: Evidence[];
    services: { unit: string; state: string; active: string; main_pid?: number }[];
  }[];
  warnings: string[];
};
const labels: Record<string, string> = {
  detected: '发现安装痕迹',
  not_detected: '常见位置未发现',
  unknown: '未知',
  present: '存在',
  absent: '未发现',
  unreadable: '无权确认',
  symlink: '符号链接（未跟随）',
  unexpected_type: '类型异常',
  available: '可找到',
  not_found: '未找到',
  unavailable: '未能查询',
  loaded: '已加载',
  masked: '已屏蔽',
  'not-found': '未找到',
  error: '错误',
  'bad-setting': '配置错误',
  active: '运行中',
  inactive: '未运行',
  failed: '失败',
  activating: '启动中',
  deactivating: '停止中',
  reloading: '重载中',
  maintenance: '维护中',
};
const label = (value: string) => labels[value] ?? '未知';
export function HostInventory() {
  const [report, setReport] = useState<Report | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  async function inspect() {
    setBusy(true);
    setError('');
    try {
      const result = await api<Report>('/host/inventory');
      if (mounted.current) setReport(result);
    } catch (e) {
      if (mounted.current) setError((e as Error).message);
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  return (
    <section className="panel host-inventory" aria-labelledby="host-inventory-title">
      <div className="section-heading">
        <div>
          <h2 id="host-inventory-title">主机与已有服务</h2>
          <p>安装向导的第一步：只读识别现有面板、核心和依赖，不自动接管。</p>
        </div>
        <button className="button secondary" onClick={() => void inspect()} disabled={busy}>
          {busy ? <CircleNotch className="spin" size={17} /> : <ArrowClockwise size={17} />}
          {busy ? '正在检测主机' : '检测已有服务'}
        </button>
      </div>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      {!report && (
        <p className="muted">
          手动检测；不会读取面板数据库、核心配置内容或证书私钥，也不会安装依赖、重启服务。
        </p>
      )}
      {report && (
        <div aria-live="polite">
          <p className="muted">
            {report.platform} ·{' '}
            {report.supported
              ? `${report.distribution} ${report.distribution_version}`
              : '仅 Linux 可用'}{' '}
            · {report.privileged ? '管理员只读视图' : '普通用户只读视图'} ·{' '}
            {timestamp(report.checked_at)}（15 秒短缓存）
          </p>
          {report.supported && (
            <>
              <p>
                TUN：{label(report.tun)} · systemd：{label(report.systemd)}
              </p>
              <div className="checks">
                {report.components.map((component) => (
                  <details className="inventory-component" key={component.id}>
                    <summary>
                      <strong>{component.name}</strong>
                      <span className={`pill ${component.state === 'detected' ? 'teal' : ''}`}>
                        {label(component.state)}
                      </span>
                    </summary>
                    <p className="muted">
                      版本：
                      {component.version === 'unknown'
                        ? '未知（未执行核心程序）'
                        : component.version}
                      。尚未验证可复用入口，不会修改现有服务。
                    </p>
                    {component.services.map((service) => (
                      <p key={service.unit}>
                        <code>{service.unit}</code> — {label(service.state)} /{' '}
                        {label(service.active)}
                        {service.main_pid ? ` · PID ${service.main_pid}` : ''}
                      </p>
                    ))}
                    <ul className="inventory-evidence">
                      {component.evidence.map((evidence) => (
                        <li key={evidence.path}>
                          <code>{evidence.path}</code>
                          <span>{label(evidence.state)}</span>
                        </li>
                      ))}
                    </ul>
                  </details>
                ))}
              </div>
              <h3>按用途检查依赖</h3>
              <div className="checks">
                {report.dependencies.map((dep) => (
                  <div className="check-row" key={dep.name}>
                    <Info size={21} />
                    <div>
                      <strong>
                        {dep.name} · {dep.purpose}
                      </strong>
                      <p>软件包提示：{dep.package_hint}；这里只检查是否可找到。</p>
                    </div>
                    <span className={`pill ${dep.state === 'available' ? 'teal' : 'amber'}`}>
                      {label(dep.state)}
                    </span>
                  </div>
                ))}
              </div>
            </>
          )}
          <ul className="inventory-warnings">
            {report.warnings.map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
