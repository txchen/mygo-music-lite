export default ({ command }: { command: string }) => ({
  name: "MyGo Music Lite",
  identifier: "dev.txchen.mygo-music-lite",
  slug: "mygo-music-lite",
  version: "0.5.1",
  updates: {
    publicKey: "XnQ//4wwe7Mh4wyzOCtmlhFM5uAgl5vXsmTvZj/m8jI=",
    github: "txchen/mygo-music-lite",
  },
  out: "build",
  urlSchemes: [command === "dev" ? "mygo-music-lite-dev" : "mygo-music-lite"],
  macos: {
    icon: "icon/AppIcon.icns",
    minimumSystemVersion: "13.0",
    signingIdentity: "-",
  },
});
