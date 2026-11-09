# Bundled audio libraries

MyGo Music Lite includes FFmpeg 9.0.2 (libavformat, libavcodec, libswresample,
libavutil), licensed under the GNU Lesser General Public License 2.1 or later.
The build does not enable GPL, nonfree or external codec components. FFmpeg is
statically linked. Its license is in `LICENSES/FFmpeg-LGPL-2.1.txt`.

miniaudio 0.11.23, copyright David Reid, is used under its MIT No Attribution
license, reproduced in `LICENSES/miniaudio-MIT-0.txt`.

Each release provides a `mygo-music-lite-VERSION-sources.tar.gz` alongside the
application. It includes this application's source, the exact unmodified FFmpeg
source archive and miniaudio header, and the dependency build script. To rebuild
or relink with modified audio libraries, extract it and follow README's Build
section. `scripts/build-audio.sh` contains the complete configure options and
source checksums. You can instead build modified FFmpeg sources with those
options into `build/audio/darwin-arm64`, then run `go tool mygo build` directly.
The development build uses ad-hoc signing and does not require our signing key.
Modification and reverse engineering for debugging library modifications are
permitted under the applicable licenses.

Upstream: <https://ffmpeg.org/> and <https://miniaud.io/>.
The application's own license is in `LICENSE`.
