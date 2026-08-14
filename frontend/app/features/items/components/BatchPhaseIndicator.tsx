import type { BatchPhase } from '../types';

// BatchPhaseIndicatorProps 描述批量流程指示器的当前步骤。
export interface BatchPhaseIndicatorProps {
  // phase 是当前批量流程步骤。
  phase: BatchPhase;
}

// BatchPhaseIndicator 展示上传、预检、发布和结果四个批量阶段。
export const BatchPhaseIndicator = ({ phase }: BatchPhaseIndicatorProps) => {
  // phases 是批量流程的固定步骤列表。
  const phases: Array<[BatchPhase, string]> = [
    ['upload', '1 上传'],
    ['preview', '2 预检'],
    ['running', '3 发布'],
    ['done', '4 结果'],
  ];
  return (
    <div className="grid grid-cols-4 gap-2">
      {phases.map(
        // 阶段渲染器根据当前步骤切换高亮样式。
        ([phaseID, label]) => (
          <div key={phaseID} className={`rounded-xl px-3 py-2 text-center text-xs font-extrabold border ${phase === phaseID ? 'bg-blue-600 text-white border-blue-600' : 'bg-gray-50 text-gray-500 border-gray-100'}`}>
            {label}
          </div>
        ),
      )}
    </div>
  );
};
