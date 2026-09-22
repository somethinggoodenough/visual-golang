import type { APIResult, Health } from '../model/types';
export async function api(path: string, body: object, signal?: AbortSignal): Promise<APIResult> {
  const response = await fetch(`/api/${path}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal: signal ?? AbortSignal.timeout(45000) });
  const data = await response.json().catch(() => null);
  if (!response.ok) throw new Error(data?.error || data?.message || data?.diagnostics?.map((d: { message: string }) => d.message).join('；') || `服务请求失败（HTTP ${response.status}）`);
  if (!data || typeof data.status !== 'string') throw new Error('服务返回了无法识别的响应。');
  return data;
}
export async function getHealth(): Promise<Health> {
  const response = await fetch('/api/health', { signal: AbortSignal.timeout(5000) });
  if (!response.ok) throw new Error('本地服务不可达');
  return response.json();
}
export function download(name: string, contents: string, type: string) {
  const url = URL.createObjectURL(new Blob([contents], { type }));
  const a = document.createElement('a'); a.href = url; a.download = name; a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
