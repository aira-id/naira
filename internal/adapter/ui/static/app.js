// Renders the IPC messages documented in RFC.md#apis (state_change,
// mouth_amplitude, window_mode, agent_status, speak_chunk) as a simple
// animated face. Mostly display-only; the one thing sent back to the
// server is an interrupt request on tap/click (see setupInterruptTap).
(() => {
  const stage = document.getElementById("stage");
  const sprite = document.getElementById("sprite");
  const mouth = document.getElementById("mouth");
  const badge = document.getElementById("agent-badge");
  const caption = document.getElementById("caption");

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
        if (msg.state === "IDLE") {
          caption.hidden = true;
          caption.textContent = "";
        }
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
      case "speak_chunk":
        caption.textContent = msg.text || "";
        caption.hidden = !msg.text;
        break;
    }
  }

  let activeWS = null;

  function connect() {
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(`${proto}://${location.host}/ws`);
    ws.onopen = () => { activeWS = ws; };
    ws.onmessage = (ev) => {
      try { handle(JSON.parse(ev.data)); } catch (_) { /* ignore malformed frame */ }
    };
    ws.onclose = () => { if (activeWS === ws) activeWS = null; setTimeout(connect, 1000); };
    ws.onerror = () => ws.close();
  }

  // Tap-to-interrupt (be-more-agent's keyboard-interrupt precedent, adapted
  // for a browser tab with no reliable keyboard focus): tapping the face
  // while THINKING/SPEAKING cancels the in-flight LLM/TTS turn.
  function setupInterruptTap() {
    stage.addEventListener("click", () => {
      const state = stage.dataset.state;
      if (state !== "THINKING" && state !== "SPEAKING") return;
      if (activeWS && activeWS.readyState === WebSocket.OPEN) {
        activeWS.send(JSON.stringify({ type: "interrupt" }));
      }
    });
  }

  setupInterruptTap();
  connect();
})();
