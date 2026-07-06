/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./*.{js,ts,jsx,tsx}",
    "./components/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        // Ydisks 官网配色：Apple 风格蓝色系 + 浅灰背景 + 深色文字。
        brand: {
          DEFAULT: '#0094f7', // 主品牌蓝（Ydisks logo/按钮色）
          dark: '#0071e3',    // 深蓝（hover/强调）
          light: '#287efe',   // 亮蓝（辅助）
        },
        // Ydisks 中性色
        ink: '#1d1d1f',       // 主文字
        muted: '#86868b',     // 次要文字
        line: '#d2d2d7',      // 边框/分隔
        canvas: '#fbfbfd',    // 页面背景
        surface: '#f5f5f7',   // 卡片背景/区块
      },
      // 圆角统一收紧到 5–10px 区间，UI 更紧凑、信息密度更高。
      borderRadius: {
        'none': '0px',
        'sm': '5px',
        DEFAULT: '6px',
        'md': '6px',
        'lg': '7px',
        'xl': '8px',
        '2xl': '10px',
        '3xl': '10px',
      },
      fontFamily: {
        sans: [
          '-apple-system',
          'BlinkMacSystemFont',
          '"PingFang SC"',
          '"Hiragino Sans GB"',
          '"Microsoft YaHei"',
          '"Helvetica Neue"',
          'Helvetica',
          'Arial',
          'sans-serif',
        ],
      },
    },
  },
  plugins: [],
}
