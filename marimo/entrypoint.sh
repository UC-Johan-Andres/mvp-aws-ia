#!/bin/bash

mkdir -p /home/marimo_user/.config/marimo

cat > /home/marimo_user/.config/marimo/marimo.toml << TOML
[ai.models]
chat_model = "openrouter/z-ai/glm-4.5-air:free"
edit_model = "openrouter/z-ai/glm-4.5-air:free"
custom_models = []
displayed_models = []

[ai.openrouter]
api_key = "${OPENROUTER_KEY}"
base_url = "https://openrouter.ai/api/v1/"
TOML

exec marimo edit --no-token -p 8080 --host 0.0.0.0
