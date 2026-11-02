import { defineConfig } from 'vitepress'
import { withMermaid } from 'vitepress-plugin-mermaid'

const repo = 'https://github.com/emdzej/acc'

export default withMermaid(defineConfig({
  title: 'ACC',
  titleTemplate: ':title · ACC',
  description: 'Agents Control Centre: one binary to watch, group and message your coding agents.',
  lang: 'en-US',
  cleanUrls: true,
  lastUpdated: true,
  mermaid: { flowchart: { wrappingWidth: 400 } },
  head: [
    ['link', { rel: 'icon', href: '/favicon.ico', sizes: 'any' }],
    ['link', { rel: 'apple-touch-icon', href: '/apple-touch-icon.png' }],
    ['meta', { name: 'theme-color', content: '#2b2f36' }],
    ['meta', { property: 'og:title', content: 'ACC: Agents Control Centre' }],
    ['meta', { property: 'og:description', content: 'Watch, group, colour and message your Claude Code sessions from a TUI or web dashboard. One Go binary.' }],
    ['meta', { property: 'og:image', content: 'https://acc.emdzej.pl/logo.png' }],
  ],
  themeConfig: {
    logo: '/logo.png',
    nav: [
      { text: 'Guide', link: '/guide/' },
      { text: 'How it works', link: '/docs/how-it-works' },
      { text: 'Reference', link: '/docs/cli' },
      { text: 'Roadmap', link: '/docs/roadmap' },
    ],
    sidebar: {
      '/guide/': [
        { text: 'Guide', items: [
          { text: 'Getting started', link: '/guide/' },
          { text: 'Dashboards (TUI & web)', link: '/guide/dashboards' },
          { text: 'Groups & colours', link: '/guide/groups' },
          { text: 'Starting sessions', link: '/guide/launching' },
          { text: 'Messaging sessions', link: '/guide/messaging' },
          { text: 'Monitoring', link: '/guide/monitoring' },
        ] },
      ],
      '/docs/': [
        { text: 'Documentation', items: [
          { text: 'How it works', link: '/docs/how-it-works' },
          { text: 'Agent integrations', link: '/docs/integrations' },
          { text: 'Security', link: '/docs/security' },
          { text: 'Roadmap', link: '/docs/roadmap' },
        ] },
        { text: 'Reference', items: [
          { text: 'CLI', link: '/docs/cli' },
          { text: 'HTTP API', link: '/docs/api' },
        ] },
      ],
    },
    outline: { level: [2, 3] },
    search: { provider: 'local' },
    socialLinks: [{ icon: 'github', link: repo }],
    editLink: { pattern: `${repo}/edit/main/site/:path`, text: 'Edit this page on GitHub' },
    footer: {
      message: `<a href="${repo}">GitHub</a> · <a href="${repo}/issues">Issues</a> · <a href="${repo}/releases">Releases</a>`,
      copyright: 'ACC: Agents Control Centre',
    },
  },
}))
