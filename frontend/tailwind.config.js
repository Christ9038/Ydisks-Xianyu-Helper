const color = (token) => `rgb(var(--color-${token}) / <alpha-value>)`;

const palette = (name) => Object.fromEntries(
  [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950]
    .map((shade) => [shade, color(`${name}-${shade}`)]),
);

/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./*.{js,ts,jsx,tsx}",
    "./components/**/*.{js,ts,jsx,tsx}",
    // 阶段七后的页面和共享 UI 位于 app/shared，必须参与生产 utility 扫描。
    "./app/**/*.{js,ts,jsx,tsx}",
    "./shared/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        // 具体色值统一维护在 index.css 的 --color-* 令牌中，组件只使用语义化 Tailwind 类名。
        transparent: 'var(--color-transparent)',
        current: 'currentColor',
        black: color('black'),
        white: color('white'),
        brand: {
          DEFAULT: color('brand'),
          highlight: color('brand-highlight'),
          light: color('brand-light'),
        },
        ink: color('ink'),
        muted: color('muted'),
        line: color('line'),
        canvas: color('canvas'),
        surface: color('surface'),
        'surface-subtle': color('surface-subtle'),
        'surface-muted': color('surface-muted'),
        success: palette('success'),
        warning: palette('warning'),
        danger: palette('danger'),
        info: palette('info'),
        // 保留现有 Tailwind 语义类，但统一指向同一套设计令牌。
        gray: palette('neutral'),
        slate: palette('slate'),
        blue: palette('brand'),
        sky: palette('brand'),
        emerald: palette('success'),
        green: palette('success'),
        amber: palette('warning'),
        orange: palette('warning'),
        red: palette('danger'),
        yellow: palette('warning'),
        purple: palette('accent'),
        pink: palette('accent'),
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
      accentColor: {
        brand: color('brand'),
      },
      boxShadow: {
        none: 'var(--shadow-none)',
        sm: 'var(--shadow-sm)',
        DEFAULT: 'var(--shadow-default)',
        md: 'var(--shadow-md)',
        lg: 'var(--shadow-lg)',
        xl: 'var(--shadow-xl)',
        '2xl': 'var(--shadow-2xl)',
        inner: 'var(--shadow-inner)',
        card: 'var(--shadow-card)',
        panel: 'var(--shadow-panel)',
        modal: 'var(--shadow-modal)',
        sidebar: 'var(--shadow-sidebar)',
        chat: 'var(--shadow-chat)',
        'chat-input': 'var(--shadow-chat-input)',
        'chat-active': 'var(--shadow-chat-active)',
        'brand-soft': 'var(--shadow-brand-soft)',
        'brand-active': 'var(--shadow-brand-active)',
        'brand-strong': 'var(--shadow-brand-strong)',
      },
    },
  },
  plugins: [],
}
