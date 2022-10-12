# Pdata

TODO

## Pdiff

- Original half-completed implementation in the old master server (where the main pdata buffer would contain modded pdata, and the master server would split/validate it) won't be used due to complexity, performance issues, and game size limitations.
- Master server will store pdata according to vanilla pdefs. This will make things much faster and more compact.
- For the tentative new pdiff implementation, the game server will deal with splitting vanilla and mod persistence into multiple pdata buffers. The master server will store and validate vanilla and modded pdata buffers separately.
