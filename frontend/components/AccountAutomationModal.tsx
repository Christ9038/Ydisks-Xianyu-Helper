import React, { useState } from 'react';
import { CalendarClock, Loader2, MessageSquareQuote, Play, Save, Sparkles, X } from 'lucide-react';
import { AccountDetail, AccountTaskSettings, AccountTaskSummary } from '../types';
import { getAccountTaskSettings, runAccountTask, updateAccountTaskSettings } from '../services/api';

interface Props {
  account: AccountDetail;
  onClose: () => void;
  onSaved: (settings: AccountTaskSettings) => void;
}

const Toggle: React.FC<{checked: boolean; onChange: () => void; label: string}> = ({ checked, onChange, label }) => (
  <button type="button" aria-label={label} aria-pressed={checked} onClick={onChange}
    className={`relative h-8 w-14 shrink-0 rounded-full transition-colors ${checked ? 'bg-sky-500' : 'bg-slate-300'}`}>
    <span className={`absolute left-1 top-1 h-6 w-6 rounded-full bg-white shadow-sm transition-transform ${checked ? 'translate-x-6' : ''}`} />
  </button>
);

const AccountAutomationModal: React.FC<Props> = ({ account, onClose, onSaved }) => {
  const [form, setForm] = useState<AccountTaskSettings>({
    account_id: account.id,
    auto_rate_enabled: account.auto_rate_enabled === true,
    rate_content: account.rate_content || '不错的买家，交易愉快',
    auto_polish_enabled: account.auto_polish_enabled === true,
    polish_time: account.polish_time || '03:00',
    last_rate_scan_at: account.last_rate_scan_at,
    last_polish_date: account.last_polish_date,
    last_polish_at: account.last_polish_at,
  });
  const [saving, setSaving] = useState(false);
  const [running, setRunning] = useState<'' | 'auto_rate' | 'auto_polish'>('');
  const [error, setError] = useState('');
  const [summary, setSummary] = useState<AccountTaskSummary | null>(null);

  const save = async () => {
    setSaving(true);
    setError('');
    try {
      const stored = await updateAccountTaskSettings(account.id, form);
      setForm(stored);
      onSaved(stored);
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const run = async (taskType: 'auto_rate' | 'auto_polish') => {
    setRunning(taskType);
    setError('');
    setSummary(null);
    try {
      await updateAccountTaskSettings(account.id, form);
      const result = await runAccountTask(account.id, taskType);
	  const stored = await getAccountTaskSettings(account.id);
      setSummary(result.summary);
	  setForm(stored);
      onSaved(stored);
    } catch (runError) {
      setError(runError instanceof Error ? runError.message : '任务执行失败');
    } finally {
      setRunning('');
    }
  };

  return (
    <div className="modal-overlay-centered">
      <div className="modal-container" style={{ maxWidth: '640px' }} role="dialog" aria-modal="true" aria-labelledby="account-task-title">
        <div className="modal-header">
          <div>
            <h3 id="account-task-title" className="text-2xl font-extrabold text-slate-950">账号自动任务</h3>
            <p className="mt-1 text-sm text-slate-500">{account.nickname || account.remark || account.id}</p>
          </div>
          <button type="button" onClick={onClose} className="rounded-xl p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700" aria-label="关闭">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="modal-body space-y-5">
          <section className="rounded-2xl border border-slate-200 bg-white p-5">
            <div className="flex items-start gap-4">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-emerald-50 text-emerald-600"><MessageSquareQuote className="h-5 w-5" /></div>
              <div className="min-w-0 flex-1">
                <div className="flex items-center justify-between gap-4">
                  <div>
                    <h4 className="font-black text-slate-900">自动评价</h4>
                    <p className="mt-1 text-xs leading-5 text-slate-500">持续扫描待评价订单，按订单执行；不是每日任务。</p>
                  </div>
                  <Toggle checked={form.auto_rate_enabled} onChange={() => setForm(current => ({ ...current, auto_rate_enabled: !current.auto_rate_enabled }))} label="自动评价" />
                </div>
                <label className="mt-4 block text-xs font-extrabold text-slate-600">统一好评文案</label>
                <textarea aria-label="统一好评文案" value={form.rate_content} maxLength={500} onChange={event => setForm(current => ({ ...current, rate_content: event.target.value }))}
                  className="mt-2 h-24 w-full resize-none rounded-xl border border-slate-200 bg-slate-50 px-3 py-2.5 text-sm leading-6 text-slate-800 placeholder:text-slate-400 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100" />
                <div className="mt-3 flex items-center justify-between gap-3">
                  <span className="text-xs text-slate-400">最近扫描：{form.last_rate_scan_at ? new Date(form.last_rate_scan_at * 1000).toLocaleString('zh-CN') : '尚未执行'}</span>
                  <button type="button" disabled={running !== '' || !account.enabled} onClick={() => void run('auto_rate')}
                    className="flex items-center gap-2 rounded-xl bg-emerald-50 px-3 py-2 text-xs font-extrabold text-emerald-700 hover:bg-emerald-100 disabled:opacity-50">
                    {running === 'auto_rate' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}立即评价
                  </button>
                </div>
              </div>
            </div>
          </section>

          <section className="rounded-2xl border border-slate-200 bg-white p-5">
            <div className="flex items-start gap-4">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-amber-50 text-amber-600"><Sparkles className="h-5 w-5" /></div>
              <div className="min-w-0 flex-1">
                <div className="flex items-center justify-between gap-4">
                  <div>
                    <h4 className="font-black text-slate-900">每日自动擦亮</h4>
                    <p className="mt-1 text-xs leading-5 text-slate-500">每个账号每天最多执行一次，按北京时间判断。</p>
                  </div>
                  <Toggle checked={form.auto_polish_enabled} onChange={() => setForm(current => ({ ...current, auto_polish_enabled: !current.auto_polish_enabled }))} label="每日自动擦亮" />
                </div>
                <div className="mt-4 flex items-end justify-between gap-4">
                  <label className="block">
                    <span className="text-xs font-extrabold text-slate-600">每日执行时间</span>
                    <input type="time" value={form.polish_time} onChange={event => setForm(current => ({ ...current, polish_time: event.target.value }))}
                      className="mt-2 block rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm font-bold text-slate-800 outline-none focus:border-sky-400" />
                  </label>
                  <button type="button" disabled={running !== '' || !account.enabled} onClick={() => void run('auto_polish')}
                    className="flex items-center gap-2 rounded-xl bg-amber-50 px-3 py-2 text-xs font-extrabold text-amber-700 hover:bg-amber-100 disabled:opacity-50">
                    {running === 'auto_polish' ? <Loader2 className="h-4 w-4 animate-spin" /> : <CalendarClock className="h-4 w-4" />}立即擦亮
                  </button>
                </div>
                <div className="mt-3 text-xs text-slate-400">上次完成：{form.last_polish_at ? new Date(form.last_polish_at * 1000).toLocaleString('zh-CN') : '尚未执行'}</div>
              </div>
            </div>
          </section>

          {summary && (
            <div className="rounded-xl border border-sky-100 bg-sky-50 px-4 py-3 text-sm text-sky-900">
              本次发现 {summary.found} 项，成功 {summary.success}，失败 {summary.failed}，跳过 {summary.skipped}。{summary.message || ''}
            </div>
          )}
          {error && <div className="rounded-xl border border-red-100 bg-red-50 px-4 py-3 text-sm font-medium text-red-700">{error}</div>}
        </div>

        <div className="modal-footer">
          <div className="flex w-full gap-3">
            <button type="button" onClick={onClose} disabled={saving || running !== ''}
              className="flex-1 rounded-xl bg-gray-100 px-6 py-3 font-bold text-gray-700 transition-colors hover:bg-gray-200 disabled:opacity-50">关闭</button>
            <button type="button" onClick={() => void save()} disabled={saving || running !== '' || (form.auto_rate_enabled && !form.rate_content.trim())}
              className="ios-btn-primary flex flex-1 items-center justify-center gap-2 rounded-xl px-6 py-3 font-bold disabled:opacity-50">
              {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}保存
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

export default AccountAutomationModal;
