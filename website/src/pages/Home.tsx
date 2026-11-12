import { For, type JSX } from 'solid-js'
import { Layout } from '../components/Layout'
import { AppWindow } from '../components/AppWindow'
import { GitHubMark, Icon, type IconName } from '../components/Icon'
import { ThemeSwitch } from '../theme'
import { mygo, repo } from '../site'
import { LangLink, Tr, useT } from '../i18n'

export function Home() {
  return (
    <Layout>
      <Hero />
      <Features />
      <Plugins />
    </Layout>
  )
}

function Hero() {
  const t = useT()
  return (
    <section class="hero">
      <div class="grooves" aria-hidden="true" />
      <div class="hero-text">
        <p class="eyebrow">
          <span class="pill">{t('Open source', '开源')}</span>
          {t('A music player for your desktop', '为桌面而做的音乐播放器')}
        </p>
        <h1 class="display hero-title">
          <Tr
            en={
              <>
                <span>Your music,</span> <span>played natively.</span>
              </>
            }
            zh={
              <>
                <span>你的音乐，</span>
                <span>原生地播放。</span>
              </>
            }
          />
        </h1>
        <p class="lede">
          <Tr
            en={
              <>
                Written in Go with <a href={mygo}>MyGo</a>, no WebView. It plays your own files out of the box, and
                everything else comes from plugins. It starts fast, stays light, and keeps quiet when nothing plays.
              </>
            }
            zh={
              <>
                用 Go 与 <a href={mygo}>MyGo</a> 写成，原生界面，没有 WebView。开箱即可播放你自己的音乐文件，其余的一切交给插件。启动快，占用少，闲下来就安安静静。
              </>
            }
          />
        </p>
        <div class="actions">
          <LangLink class="btn btn-primary" to="/download">
            <Icon name="download" size={17} />
            {t('Download', '下载')}
          </LangLink>
          <a class="btn btn-ghost" href={repo}>
            <GitHubMark size={17} />
            {t('View Source', '查看源代码')}
          </a>
        </div>
        <p class="fine">
          {t(
            'Free · Open source under AGPL-3.0 · macOS, Windows and Linux',
            '免费 · 以 AGPL-3.0 开源 · macOS、Windows 与 Linux',
          )}
        </p>
      </div>
      <div class="hero-window">
        <AppWindow />
        <p class="hint">{t('Try playing the song, or liking it.', '试试点一下播放，或者喜欢这首歌。')}</p>
      </div>
    </section>
  )
}

// Each text is in English and in Chinese.
type Feature = {
  icon: IconName
  title: [string, string]
  body: [string, string]
  wide?: boolean
  extra?: () => JSX.Element
}

const features: Feature[] = [
  {
    icon: 'folder',
    title: ['Your files, as they are', '你的文件，原样就好'],
    body: [
      'Add the folders where your music lives and it’s all there: MP3, FLAC and Ogg, with their tags, covers and .lrc lyrics, sorted into songs, albums and artists. Make playlists, like songs, search.',
      '添加存放音乐的文件夹，一切就都在了：MP3、FLAC 与 Ogg，连同标签、封面与 .lrc 歌词，按歌曲、专辑、歌手排好。建歌单、喜欢、搜索，样样都行。',
    ],
    wide: true,
    extra: () => <Files />,
  },
  {
    icon: 'bolt',
    title: ['Native, and quiet', '原生，而且安静'],
    body: [
      'Drawn by MyGo, with no browser inside. When nothing moves, it draws nothing.',
      '由 MyGo 绘制，里面没有浏览器。画面不动时，它什么也不画。',
    ],
  },
  {
    icon: 'volume',
    title: ['A gentle volume', '温柔的音量'],
    body: [
      'Fades out and back in on pause and seek, and evens out loudness from song to song.',
      '暂停与跳转时淡出淡入，歌与歌之间音量均衡。',
    ],
    extra: () => <Fade />,
  },
  {
    icon: 'notch',
    title: ['Up in the notch', '刘海播放器'],
    body: [
      'A mini player lives in the Mac’s notch and opens as the pointer comes by. There’s a tray menu, and the media keys work.',
      '在 Mac 屏幕顶部的刘海处显示迷你播放器，指针移过去就展开。还有托盘菜单，媒体键也能用。',
    ],
  },
  {
    icon: 'history',
    title: ['Where you left off', '从上次停下的地方'],
    body: [
      'Quit, open it again, and the queue and the position in the song are back.',
      '退出后再打开，播放队列与进度都还在。',
    ],
  },
  {
    icon: 'lyrics',
    title: ['Lyrics on a stage', '「律动」歌词'],
    body: [
      'Now Playing lights up the lyrics word by word, in a choice of stage styles, or shows a wall of covers. The queue, shuffle and repeat are a click away.',
      '「正在播放」里，歌词逐字亮起，律动效果任你挑选；也可以换成满屏的封面墙。播放队列、随机与循环，一点即达。',
    ],
    wide: true,
  },
  {
    icon: 'disk',
    title: ['Kept on the device', '听过的，存在本机'],
    body: [
      'Songs streamed by a plugin are kept, to play again at once, without the network.',
      '插件播放过的歌保存在本机，再次播放无需等待，也不耗流量。',
    ],
  },
  {
    icon: 'captions',
    title: ['Captions for podcasts', '播客字幕'],
    body: [
      'Episodes are written down on your computer as they play, with sherpa-onnx and Qwen3-ASR. Nothing leaves it.',
      '边播边在你的电脑上生成字幕，用的是 sherpa-onnx 与 Qwen3-ASR，什么都不会离开本机。',
    ],
    wide: true,
  },
  {
    icon: 'sun',
    title: ['Light, dark, or the system', '浅色、深色、跟随系统'],
    body: ['Just like this website. Give it a try.', '和这个网站一样——试试看。'],
    extra: () => <ThemeSwitch labels class="feature-theme" />,
  },
]

function Features() {
  const t = useT()
  return (
    <section id="features" class="section">
      <div class="section-head">
        <p class="eyebrow">{t('Features', '功能')}</p>
        <h2 class="display">
          <Tr
            en={
              <>
                <span>All you’d expect,</span> <span>and then some.</span>
              </>
            }
            zh={
              <>
                <span>该有的，</span>
                <span>都在。</span>
              </>
            }
          />
        </h2>
        <p class="lede">
          {t(
            'Made for the desktop from the start. Liked songs, playlists, a queue, shuffle and repeat, all where you’d look for them.',
            '从一开始就为桌面而做。喜欢、歌单、播放队列、随机与循环，都在你想找的地方。',
          )}
        </p>
      </div>
      <div class="features">
        <For each={features}>
          {(f) => (
            <article class="feature" classList={{ wide: f.wide }}>
              <div class="feature-icon">
                <Icon name={f.icon} size={20} />
              </div>
              <h3>{t(...f.title)}</h3>
              <p>{t(...f.body)}</p>
              {f.extra?.()}
            </article>
          )}
        </For>
      </div>
    </section>
  )
}

// A folder of music as the app reads it: the song, its cover and its
// lyrics, beside it.
function Files() {
  const t = useT()
  const files: { name: string; kind: [string, string]; icon: IconName }[] = [
    { name: 'Polar Night/01 Northern Lights.flac', kind: ['FLAC · 3:00', 'FLAC · 3:00'], icon: 'music' },
    { name: 'Polar Night/01 Northern Lights.lrc', kind: ['Lyrics', '歌词'], icon: 'lyrics' },
    { name: 'Polar Night/cover.jpg', kind: ['Cover', '封面'], icon: 'album' },
  ]
  return (
    <ul class="files" aria-label={t('A folder of music', '一个音乐文件夹')}>
      <For each={files}>
        {(f) => (
          <li>
            <Icon name={f.icon} size={14} />
            <span class="files-name">{f.name}</span>
            <span class="files-kind">{t(...f.kind)}</span>
          </li>
        )}
      </For>
    </ul>
  )
}

// A wave fading out at a pause and back in, drawn.
function Fade() {
  const t = useT()
  const bars = Array.from({ length: 44 }, (_, i) => {
    const env = i < 16 ? 1 : i < 22 ? 1 - (i - 16) / 6 : i < 26 ? 0 : Math.min((i - 26) / 6, 1)
    return Math.max(0.06, env * (0.45 + 0.55 * Math.abs(Math.sin(i * 1.7) * Math.cos(i * 0.6))))
  })
  return (
    <div class="fade" aria-hidden="true">
      <For each={bars}>{(h) => <i style={{ height: `${h * 100}%` }} />}</For>
      <span class="fade-mark">{t('Pause', '暂停')}</span>
    </div>
  )
}

const manifest = `{
  "id": "example",
  "name": "Example",
  "version": "1.0.0",
  "main": "index.js",
  "playback": "stream",
  "permissions": {
    "network": ["api.example.com"]
  }
}`

const script = `export async function search({ query }) {
  const res = await fetch(
    \`https://api.example.com/search?q=\${encodeURIComponent(query)}\`,
  )
  return { tracks: (await res.json()).items, more: false }
}

export function stream({ id }) {
  return { url: \`https://api.example.com/audio/\${id}.mp3\`, type: 'mp3' }
}`

const abilities: { icon: IconName; text: [string, string] }[] = [
  { icon: 'artist', text: ['Accounts, with a QR code or a browser to sign in', '账号，扫码或在浏览器中登录'] },
  { icon: 'album', text: ['Catalogs: search, albums, artists, playlists and lyrics', '曲库：搜索、专辑、歌手、歌单与歌词'] },
  { icon: 'radio', text: ['Podcasts, stations, comments, and pages of their own', '播客、电台、评论，还有自己的页面'] },
  { icon: 'download', text: ['Installed from GitHub, and checked for updates daily', '从 GitHub 安装，每天检查更新'] },
]

function Plugins() {
  const t = useT()
  return (
    <section id="plugins" class="section">
      <div class="section-head">
        <p class="eyebrow">{t('Plugins', '插件')}</p>
        <h2 class="display">
          <Tr
            en={
              <>
                <span>Everything else,</span> <span>from plugins.</span>
              </>
            }
            zh={
              <>
                <span>其余的一切，</span>
                <span>交给插件。</span>
              </>
            }
          />
        </h2>
        <p class="lede">
          <Tr
            en={
              <>
                A plugin is a folder with a <code>plugin.json</code> and one JavaScript or TypeScript file. There’s
                nothing to build, and the same plugin runs on every platform. It reaches only the hosts it names, and
                asks before it starts a program.
              </>
            }
            zh={
              <>
                插件就是一个文件夹，里面有一个 <code>plugin.json</code> 和一个 JavaScript 或 TypeScript 文件。无需构建，同一个插件在每个平台上都能运行。它只能访问自己声明的网站，启动程序前也会先征得你的同意。
              </>
            }
          />
        </p>
      </div>
      <div class="plugins">
        <div class="code">
          <Code name="plugin.json" text={manifest} />
          <Code name="index.js" text={script} />
        </div>
        <div class="plugin-side">
          <ul class="abilities">
            <For each={abilities}>
              {(a) => (
                <li>
                  <span class="feature-icon">
                    <Icon name={a.icon} size={18} />
                  </span>
                  {t(...a.text)}
                </li>
              )}
            </For>
          </ul>
          <div class="actions">
            <a class="btn btn-primary" href={`${repo}#plugins`}>
              {t('Read the Guide', '阅读插件指南')}
              <Icon name="arrow" size={16} />
            </a>
            <LangLink class="btn btn-ghost" to="/plugins">
              <Icon name="plugin" size={17} />
              {t('Browse Plugins', '浏览插件')}
            </LangLink>
            <a class="btn btn-ghost" href={`${repo}/tree/main/examples/demo-plugin`}>
              {t('Demo Plugin', '示例插件')}
            </a>
          </div>
        </div>
      </div>
    </section>
  )
}

// Strings, keywords and property names, picked out of a little code.
const tokens = /("(?:[^"\\]|\\.)*"(?=\s*:))|("(?:[^"\\]|\\.)*"|'[^']*'|`(?:[^`\\]|\\.)*`)|\b(export|async|function|const|await|return)\b/g

function Code(props: { name: string; text: string }) {
  const parts: JSX.Element[] = []
  let last = 0
  for (const m of props.text.matchAll(tokens)) {
    parts.push(props.text.slice(last, m.index))
    const kind = m[1] ? 'tk-key' : m[2] ? 'tk-str' : 'tk-kw'
    parts.push(<span class={kind}>{m[0]}</span>)
    last = m.index + m[0].length
  }
  parts.push(props.text.slice(last))
  return (
    <figure class="code-file">
      <figcaption>{props.name}</figcaption>
      <pre>
        <code>{parts}</code>
      </pre>
    </figure>
  )
}
