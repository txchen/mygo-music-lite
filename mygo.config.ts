export default ({ command }: { command: string }) => ({
  name: "MyGo Music Lite",
  identifier: "dev.txchen.mygo-music-lite",
  slug: "mygo-music-lite",
  version: "0.4.5",
  out: "build",
  urlSchemes: [command === "dev" ? "mygo-music-lite-dev" : "mygo-music-lite"],
  macos: {
    icon: "icon/AppIcon.icns",
    minimumSystemVersion: "13.0",
    signingIdentity: "-",
  },
});
