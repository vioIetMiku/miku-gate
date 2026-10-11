import { useState } from 'react';
import { CircleNotch, Info } from '@phosphor-icons/react';
import { api, reputationLabel, timestamp, type IPIntelligence as Report } from './api';
import { Modal } from './Modal';

type Selection = { kind: 'node' | 'exit'; id: string; name: string; ip?: string };
export function IPIntelligence({ selection, close }: { selection: Selection; close: () => void }) {
  const [consent, setConsent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [result, setResult] = useState<Report | null>(null);
  const [hostname, setHostname] = useState('');
  const [resolvedDomain, setResolvedDomain] = useState('');
  async function lookup(domainLookup = false) {
    setBusy(true);
    setError('');
    setResult(null);
    setResolvedDomain('');
    try {
      if (domainLookup) {
        const response = await api<{ hostname: string; report: Report; warning: string }>(
          '/intelligence/domain',
          'POST',
          { hostname, confirm: consent },
        );
        setResult(response.report);
        setResolvedDomain(response.hostname);
      } else {
        setResult(
          await api<Report>(
            `/${selection.kind === 'node' ? 'nodes' : 'exits'}/${encodeURIComponent(selection.id)}/intelligence`,
            'POST',
            { confirm: consent },
          ),
        );
      }
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title={`归属与风险辅助 · ${selection.name}`} close={close}>
      <div className="modal-form">
        <div className="notice">
          <Info size={20} />
          <p>
            查询会把目标IP发送给RIPEstat和IPWHOIS.IO，走VPS宿主HTTPS。不是业务直连，也不是VPN连通性检测。候选IP和实际出口IP可能不同；不承诺住宅、独享、解锁或纯净。
          </p>
        </div>
        <label className="checkbox-line">
          <input type="checkbox" checked={consent} onChange={(e) => setConsent(e.target.checked)} />
          我同意向固定第三方查询IP信息（不发送账户或代理密码）
        </label>
        <div className="modal-actions">
          <button
            className="button primary"
            disabled={!consent || busy}
            onClick={() => void lookup()}
          >
            {busy ? <CircleNotch className="spin" size={17} /> : null}
            {selection.kind === 'node' ? `查询候选 ${selection.ip ?? ''}` : '查询当前实测健康出口'}
          </button>
        </div>
        <label>
          也可查域名解析IP（不是经营者归属）
          <input
            value={hostname}
            onChange={(e) => setHostname(e.target.value)}
            placeholder="www.cloudflare.com"
            autoComplete="off"
            maxLength={253}
          />
        </label>
        <button
          className="button"
          disabled={!consent || busy || !hostname}
          onClick={() => void lookup(true)}
        >
          查询域名首个公共IPv4
        </button>
        {error && (
          <div className="inline-error" role="alert">
            {error}
          </div>
        )}
        {result && (
          <>
            <dl className="credential-list">
              <div>
                <dt>查询地址</dt>
                <dd>
                  <code>{result.ip}</code>
                  {resolvedDomain ? ` · ${resolvedDomain}` : ''}
                </dd>
              </div>
              <div>
                <dt>查询时间</dt>
                <dd>
                  {timestamp(result.checked_at)}
                  {result.cached ? ' · 缓存结果' : ''}
                </dd>
              </div>
              <div>
                <dt>IP地理库</dt>
                <dd>
                  {result.geography.state === 'available'
                    ? [result.geography.country, result.geography.region, result.geography.city]
                        .filter(Boolean)
                        .join(' / ')
                    : '未知'}
                  <small className="subline">{result.geography.source}</small>
                </dd>
              </div>
              <div>
                <dt>运营商 / 组织</dt>
                <dd>
                  {[result.geography.isp, result.geography.organization]
                    .filter(Boolean)
                    .join(' / ') || '未知'}
                </dd>
              </div>
              <div>
                <dt>BGP前缀</dt>
                <dd>{result.prefix ?? '未知'}</dd>
              </div>
              <div>
                <dt>宣告ASN</dt>
                <dd>
                  {result.asns.length
                    ? result.asns.map((asn) => (
                        <div key={asn.number}>
                          <code>AS{asn.number}</code> {asn.holder || '运营商名称未知'}
                          <small className="subline">{asn.source}</small>
                        </div>
                      ))
                    : '未知'}
                </dd>
              </div>
              <div>
                <dt>住宅 / 独享</dt>
                <dd>未知（不凭运营商名称或国家猜测）</dd>
              </div>
              <div>
                <dt>黑名单辅助</dt>
                <dd>
                  {reputationLabel(result.reputation.state)}
                  <small className="subline">
                    {result.reputation.source} · 已完成 {result.reputation.checked_count} 项 · 命中{' '}
                    {result.reputation.listed_count} 项
                  </small>
                </dd>
              </div>
            </dl>
            {result.reputation.results.length > 0 && (
              <ul className="capability-list">
                {result.reputation.results.map((record) => (
                  <li key={record.id}>
                    {record.operator} {record.name} ·{' '}
                    {record.status === 'listed'
                      ? '列出'
                      : record.status === 'notlisted'
                        ? '本来源未列出'
                        : '未知'}
                    {record.query_time ? ` · ${record.query_time}` : ''}
                  </li>
                ))}
              </ul>
            )}
            {result.warnings.map((warning) => (
              <p className="field-note" key={warning}>
                {warning}
              </p>
            ))}
            {resolvedDomain && (
              <p className="field-note">
                域名可能使用CDN/Anycast，只查第一个排序的公共IPv4，不代表网站经营者所在地或所有权。
              </p>
            )}
            <p className="field-note">
              查询路径：{result.network_path}
              。普通结果缓存15分钟，待完成/无数据短缓存1分钟；不自动循环查询。
            </p>
          </>
        )}
        <div className="modal-actions">
          <button className="button" disabled={busy} onClick={close}>
            关闭
          </button>
        </div>
      </div>
    </Modal>
  );
}
