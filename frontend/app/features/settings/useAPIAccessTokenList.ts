import { useCallback, useEffect, useRef, useState } from 'react';
import { listAPIAccessTokens, type APIAccessTokenSummary } from './api';

// useAPIAccessTokenList 统一负责令牌列表的最新请求胜出、错误状态和卸载后的响应隔离。
export const useAPIAccessTokenList = () => {
  // tokens 保存当前用户的令牌摘要，不包含任何明文令牌。
  const [tokens, setTokens] = useState<APIAccessTokenSummary[]>([]);
  // loading 表示最新一代列表读取是否尚未返回。
  const [loading, setLoading] = useState(false);
  // loadError 仅保存最新一代请求的读取错误。
  const [loadError, setLoadError] = useState('');
  // requestGeneration 标识最近一次列表读取，签发或撤销后的刷新会废弃旧结果。
  const requestGeneration = useRef(0);
  // mounted 标识当前 Hook 是否仍在页面中，卸载后不得写入状态。
  const mounted = useRef(false);

  // loadTokens 每次启动新一代读取，仅提交最新请求返回的令牌、加载状态和错误。
  const loadTokens = useCallback(/* requestLatestList 仅提交最新代次结果，过期代次不改变状态。 */ async () => {
    // generation 是本次列表读取对应的唯一代次。
    const generation = ++requestGeneration.current;
    setLoading(true);
    setLoadError('');
    try {
      // response 是当前用户的令牌摘要，不包含签发时的一次性明文。
      const response = await listAPIAccessTokens();
      if (mounted.current && generation === requestGeneration.current) setTokens(response.data);
    } catch {
      if (mounted.current && generation === requestGeneration.current) setLoadError('读取 API 访问令牌失败，请刷新后重试。');
    } finally {
      if (mounted.current && generation === requestGeneration.current) setLoading(false);
    }
  }, []);

  // 仅在页面挂载期间允许读取结果提交；卸载时立即使所有未完成请求失效。
  useEffect(/* mountListReader 装载期间读取数据并登记卸载失效边界。 */ () => {
    mounted.current = true;
    void loadTokens();
    return /* invalidateUnmountedReads 卸载时废弃仍在途的所有旧代次。 */ () => {
      mounted.current = false;
      requestGeneration.current += 1;
    };
  }, [loadTokens]);

  return { tokens, loading, loadError, loadTokens };
};
