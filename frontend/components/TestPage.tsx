import React from 'react';

const TestPage: React.FC = () => {
  return (
    <div className="min-h-screen bg-gray-100 flex items-center justify-center">
      <div className="bg-white p-8 rounded-2xl shadow-lg">
        <h1 className="text-3xl font-bold text-gray-900 mb-4">测试页面</h1>
        <p className="text-gray-600 mb-4">如果你能看到这个页面，说明基础功能正常。</p>
        <div className="space-y-2">
          <div className="p-4 bg-blue-100 rounded-lg text-blue-800">蓝色卡片</div>
          <div className="p-4 bg-green-100 rounded-lg text-green-800">绿色卡片</div>
          <div className="p-4 bg-blue-100 rounded-lg text-blue-800">黄色卡片</div>
        </div>
      </div>
    </div>
  );
};

export default TestPage;
