import { useCallback, useEffect, useRef, useState } from 'react';
import { uid, type Diagnostic } from '../model/types';
import type { AnalysisResult, RunResult } from './model';

async function post<T extends { requestId: string }>(path: string, source: string, signal: AbortSignal, extra: object = {}): Promise<T> {
  const requestId = uid('explore');
  const response = await fetch(`/api/${path}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ requestId, source, ...extra }), signal });
  const body = await response.json();
  if (!response.ok) throw new Error(body.diagnostics?.map((d: Diagnostic) => d.message).join('\n') || `HTTP ${response.status}`);
  if (body.requestId !== requestId) throw new Error('Unexpected response request ID');
  return body as T;
}

export function useExplorer(source: string) {
  const [analysis, setAnalysis] = useState<AnalysisResult | null>(null);
  const [result, setResult] = useState<RunResult | null>(null);
  const [diagnostics, setDiagnostics] = useState<Diagnostic[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState('');
  const sourceRef = useRef(source); sourceRef.current = source;
  const controller = useRef<AbortController | null>(null);
  const sequence = useRef(0);
  useEffect(() => {
    sequence.current++; controller.current?.abort(); controller.current = null;
    setAnalysis(null); setResult(null); setDiagnostics([]); setBusy(null); setError('');
    return () => { sequence.current++; controller.current?.abort(); };
  }, [source]);

  const cancel = useCallback(() => {
    sequence.current++; controller.current?.abort(); controller.current = null; setBusy(null);
    setError('操作已停止。');
  }, []);

  const perform = useCallback(async (mode: 'analyze' | 'build-source' | 'run' | 'trace') => {
    controller.current?.abort(); const active = new AbortController(); controller.current = active;
    const ticket = ++sequence.current, text = sourceRef.current;
    setBusy(mode); setDiagnostics([]); setError('');
    if (mode !== 'analyze') setResult(null);
    const current = () => ticket === sequence.current && text === sourceRef.current && !active.signal.aborted;
    try {
      const inspected = await post<AnalysisResult>('analyze', text, active.signal);
      if (!current()) return;
      setAnalysis(inspected); setDiagnostics(inspected.diagnostics);
      // Type diagnostics need not prevent an actual compiler check. Analysis
      // may be unable to resolve a standard library supported by go build.
      if (mode !== 'analyze') {
        const executed = await post<RunResult>(mode === 'build-source' ? mode : 'run', text, active.signal, { trace: mode === 'trace' });
        if (!current()) return;
        if (executed.sourceHash !== inspected.sourceHash) throw new Error('Source hash mismatch');
        setResult(executed); setDiagnostics(executed.diagnostics ?? []);
      }
    } catch (reason) {
      if (current()) setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      if (current()) { setBusy(null); controller.current = null; }
    }
  }, []);
  return { analysis, result, diagnostics, busy, error, perform, cancel, setDiagnostics };
}
