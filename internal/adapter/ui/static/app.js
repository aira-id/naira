// Renders the IPC messages documented in RFC.md#apis (state_change,
// mouth_amplitude, window_mode, agent_status) as a simple animated face.
// Display-only client: never sends anything meaningful back to the server.
(() => {
  const stage = document.getElementById("stage");
  const sprite = document.getElementById("sprite");
  const mouth = document.getElementById("mouth");
  const badge = document.getElementById("agent-badge");

  // Real drawn sprite frames (assets/faces/) — only 4 of the 9
  // domain.ExpressionTag states have art yet; the rest fall back to the
  // CSS-drawn #face (data-has-sprite="false", see style.css). SPEAKING's
  // frame is picked by mouth_amplitude instead of a fixed interval so the
  // mouth actually syncs to TTS output.
  const SPRITES = {
    IDLE: { frames: ["/faces/idle-01.png"], intervalMs: 0 },
    LISTENING: { frames: ["/faces/listening-01.png", "/faces/listening-02.png"], intervalMs: 600 },
    THINKING: { frames: ["/faces/thinking-01.png", "/faces/thinking-02.png", "/faces/thinking-03.png", "/faces/thinking-04.png"], intervalMs: 400 },
    SPEAKING: { frames: ["/faces/speaking-01.png", "/faces/speaking-02.png", "/faces/speaking-03.png"], amplitudeDriven: true },
  };

  let badgeTimer = null;
  let animTimer = null;
  let animFrame = 0;

  function stopAnim() {
    clearInterval(animTimer);
    animTimer = null;
  }

  function applyState(state) {
    const set = SPRITES[state];
    stage.dataset.hasSprite = set ? "true" : "false";
    stopAnim();
    if (!set) return;

    animFrame = 0;
    sprite.src = set.frames[0];
    if (set.frames.length > 1 && !set.amplitudeDriven) {
      animTimer = setInterval(() => {
        animFrame = (animFrame + 1) % set.frames.length;
        sprite.src = set.frames[animFrame];
      }, set.intervalMs);
    }
  }

  function applyAmplitude(amp) {
    const set = SPRITES[stage.dataset.state];
    if (!set || !set.amplitudeDriven) return;
    const idx = Math.min(set.frames.length - 1, Math.floor(amp * set.frames.length));
    sprite.src = set.frames[idx];
  }

  function handle(msg) {
    switch (msg.type) {
      case "state_change":
        stage.dataset.state = msg.state;
        applyState(msg.state);
        break;
      case "mouth_amplitude": {
        const amp = Math.max(0, Math.min(1, msg.amplitude));
        mouth.style.setProperty("--amp", amp.toFixed(3));
        applyAmplitude(amp);
        break;
      }
      case "window_mode":
        stage.dataset.mode = msg.mode;
        break;
      case "agent_status":
        badge.textContent = msg.status;
        badge.dataset.status = msg.status;
        badge.hidden = false;
        clearTimeout(badgeTimer);
        if (msg.status === "DONE" || msg.status === "FAILED") {
          badgeTimer = setTimeout(() => { badge.hidden = true; }, 4000);
        }
        break;
    }
  }

  function connect() {
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(`${proto}://${location.host}/ws`);
    ws.onmessage = (ev) => {
      try { handle(JSON.parse(ev.data)); } catch (_) { /* ignore malformed frame */ }
    };
    ws.onclose = () => setTimeout(connect, 1000);
    ws.onerror = () => ws.close();
  }

  connect();
})();
