# Generated media fixtures

These files contain synthetic images/video generated locally with FFmpeg 7.1.
They contain no personal data and require no WhatsApp account. FFmpeg is only
used to reproduce fixtures, never by the server or automated tests.

```sh
ffmpeg -f lavfi -i color=c=blue:s=32x32:r=5 -t 0.4 -an -c:v libx264 -pix_fmt yuv420p -movflags +faststart video.mp4
ffmpeg -f lavfi -i color=c=blue:s=32x32:r=5 -f lavfi -i sine=frequency=440:sample_rate=44100 -t 0.4 -c:v libx264 -pix_fmt yuv420p -c:a aac -movflags +faststart video-audio.mp4
ffmpeg -f lavfi -i color=c=blue:s=512x512 -frames:v 1 -c:v libwebp -lossless 1 sticker.webp
ffmpeg -f lavfi -i testsrc2=size=512x512:rate=2 -t 1 -c:v libwebp_anim -lossless 0 -quality 20 -loop 0 animated.webp
```
