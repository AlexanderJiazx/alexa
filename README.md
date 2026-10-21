# Alexa

Alexa is an always-on personal agent, in the spirit of Muse. (It is not Amazon's Alexa; the name is just shared.) You tell it what you want, by typing or talking, and it gets the job done on the web. It researches with live web search, drives its own Chrome, fills in forms, reads PDFs, writes files, runs code and remembers what you like. It also runs jobs on a schedule and pings you when it needs you or when it's done.

There are no chats or tasks to manage. Each person has one conversation with Alexa that never ends. Requests, follow-ups, answers and scheduled jobs all land in it. Older parts are folded into a running summary as it grows, so it can run forever.

- **Backend:** Go, as a single binary with the UI embedded. It uses SQLite, chromedp and the Meta Model API.
- **Frontend:** React 19 + Vite, built to the "Paper" design mock-up.
- **Model:** `muse-spark-1.3-contributor` via the Meta Model API's Responses endpoint, with native web search.
- **Voice:** live transcription through Meta's realtime ASR (`muse-voice-transcribe-1.0`). The fallbacks are the browser's speech recognition, then recorded audio sent to Meta's batch transcription.

## What it can do

- **One conversation, forever.** When the prompt passes `COMPACT_TOKENS`, Alexa folds the older part into a structured summary and carries on. You can also do this on demand under *Settings → Conversation*. Lasting facts go to memory. Your feed keeps everything, so nothing is lost.
- **The screen shows what's happening now.** The main screen holds only the current exchange: the request that set Alexa to work and everything since. A new request clears it, and a finished exchange you've seen is put away when you come back more than 30 minutes later (or tap the dot, top left). *Reflect* (the clock icon) is where the past lives: the whole conversation, every report and your files.
- **Speaks only when it has something to say.** The model's own text and reasoning stay hidden. Alexa talks to you through a `say` tool, in one or two short sentences (it's told to stay under 300 characters, and anything over 400 is refused). Questions come through `ask`, with quick-reply buttons.
- **Longer answers become reports.** Research, comparisons, itineraries and plans open on screen as a document. A report is markdown, or a sandboxed interactive HTML page for charts and the like. It comes with a download button and *Add to calendar* (.ics) when it has events. Every report is kept in *Reflect*.
- **The screen follows the agent.** While Alexa works in its browser, the live browser slides in beside the conversation, and Alexa's pointer glides to whatever it clicks before the page reacts. A new report opens by itself. If you choose what to look at during a request, it stops switching your screen until your next message. What's on screen is stored on the server, so every device you have open shows the same thing, and it's still there after a reload.
- **Multi-step tasks in a real browser.** The agent sees each page as a numbered list of interactive elements plus an annotated screenshot, including what's inside embedded frames (same-site and cross-site, such as booking widgets and consent banners). It clicks, types, hovers, drags, scrolls, selects and uploads, and it follows popups and comes back when they close. Downloads land in `downloads/`, and the agent is told what each one saved; a download never replaces a file with the same name (it becomes `bill-2.pdf`). Date, time and slider fields get their values set directly. A page's confirm or prompt dialog waits for the agent to answer it instead of being clicked through.
- **Take over.** Press *Take over* to drive the same browser yourself, then *Hand back*: click and scroll in it, type on your keyboard as you would on the page (on a phone, tap the keyboard button), or use the URL bar. Alexa asks for this when it hits a 2FA code, a CAPTCHA or payment details.
- **Signs in for you.** When a site asks Alexa to sign in, including when a session has expired, it signs in with your saved login for that site. If there isn't one, it asks you on screen with a secure form: you type your username and password there and can save them for next time. Alexa's model never sees a password. Passwords are encrypted (AES-256-GCM) with a key kept outside the database, are never sent back to the app, and only go into a password field on a page of the site they belong to, over HTTPS. That's checked from an isolated world the page's own scripts can't reach, so a page can't pass itself off as another site. If a page later shows a password, it's kept from the model, screenshots included. You can add, change and delete logins under *Logins*.
- **Asks before acting.** It confirms before purchases, sending messages or other consequential steps.
- **Talk while it works.** Messages you send mid-job are picked up at its next step. A message also cuts short any wait in progress.
- **Web search** with sources, plus `fetch_url` for quick reads.
- **Files.** Attach files to a message; the agent reads images and PDFs directly. They keep their names, so a site Alexa uploads one to sees `lease.pdf`, not a made-up name. It can also look at images and PDF pages in its workspace, so it can check an image it generated or read a scanned document it downloaded. It can write files to your workspace, download files, and run shell commands in your private workspace: Python with pandas, matplotlib, openpyxl and python-docx, plus `pdftotext`, `tesseract` (OCR), `ffmpeg`, `jq` and `curl`.
- **Memory.** It remembers facts about you; you can view and edit them under *Memory*.
- **Schedules.** Say "every weekday at 7:30, …" or "tomorrow at 9, …", or add one under *Scheduled*. A scheduled job arrives in the same conversation as a message from the scheduler. It works in the background: it doesn't switch your screen to the browser, and a job that has nothing to tell you (a price watch that found no drop) stays off the main screen, which keeps showing what you were looking at.
- **Notifications:** Web Push (installable PWA) and, optionally, email via Resend. They are sent only when you don't have Alexa open on screen.
- **Multiple users.** Each person gets their own conversation, memory, files, reports, schedules and Chrome profile, so cookies and logins stay separate. In the container, each user's Chrome and shell also run under their own Linux UID, so users can't read each other's files or the database. (Outside Docker, run as root with these UIDs, they must be able to reach their folders: every directory above `DATA_DIR` needs `o+x`, and Alexa won't start and says which ones if they don't.)
- **Voice mode.** Tap the bars, speak, and it sends when you stop talking. (If Meta's realtime transcription can't be reached, Alexa uses the browser's own speech recognition, or records you and transcribes when you tap the circle; it keeps the audio from the moment you tap, so the start of what you said isn't lost.) What you say shows at the bottom of the screen, so the conversation and the browser stay in view. When you asked by voice, it reads its reply back.
- **Light and dark.** The app follows your device's appearance; you can pin light or dark under *Settings → Appearance*.
- **Survives restarts.** Work that was in progress when the server stopped picks up again when it starts.

## Run it on a Raspberry Pi (Docker)

You need a Pi 4 or 5 with 4 GB of RAM or more, running a 64-bit OS, plus Docker with the compose plugin.

```bash
git clone <this repo> alexa && cd alexa
cp .env.example .env          # set META_MODEL_API_KEY (and PUBLIC_URL)
docker compose up -d --build  # builds natively for arm64; the first build takes a while
```

Open `http://<pi>:8080`. The first account you create becomes the admin, and the admin adds everyone else (*Users* in the account menu). Set `ALLOW_SIGNUP=true` to let people sign themselves up.

All state lives in `./data`: the SQLite DB (`alexa.db`; a `paper.db` from before the rename is picked up automatically), the key saved logins are encrypted with (`vault.key`, made on first start), plus each user's workspace, browser profile and screenshots. Back up that folder to back everything up. Without `vault.key` the saved passwords can't be read, so keep it safe; to keep it apart from the database, set `VAULT_KEY_FILE` to a file elsewhere (a Docker secret, for example).

### HTTPS, push notifications and the microphone

Browsers allow the microphone and Web Push only on `https://` or `localhost`. To use Alexa from your phone, put it behind HTTPS and set `SECURE_COOKIES=true` and `PUBLIC_URL=https://…`. Any of these works:

- **Tailscale:** `tailscale serve --bg 8080` gives you `https://<pi>.<tailnet>.ts.net`.
- **Caddy:** `alexa.example.com { reverse_proxy localhost:8080 }`.
- **Cloudflare Tunnel.**

On iPhone, open the site in Safari, then *Share → Add to Home Screen*. Then enable notifications in *Settings → Notifications*.

## Run it locally (development)

```bash
export META_MODEL_API_KEY=...
cd web && npm install && npm run build && cd ..
go run ./cmd/alexagent -debug          # http://localhost:8080 (needs Chrome installed)
```

Run this way, as you rather than as root, there are no per-user UIDs: the agent's shell commands run as you and can read anything you can, the data folder and `vault.key` included. Use the Docker setup for real accounts and saved logins.

For UI work, run `npm run dev` in `web/` (Vite on :5173, which proxies `/api` to `127.0.0.1:8090`) with the server on `ADDR=127.0.0.1:8090`.

Command-line helpers:

```bash
./alexagent -create-user alice:password:admin
python3 scripts/alexactl.py send "What's on the front page of Hacker News?"
```

`alexactl.py` is a small API client used for end-to-end tests. It signs in with `ALEXA_URL`, `ALEXA_USER` and `ALEXA_PASSWORD` (or `--url`, `--user` and `--password`). `send` follows Alexa's reply as it comes in, printing its steps, messages and reports. It takes `--attach file`, `--answer text` (to reply to its first question), `--handback` and `--login user:password` (to answer its first sign-in request; the login is saved). The other commands are `feed`, `report ID`, `stop`, `compact`, `memories`, `schedules` and `logins`.

## Configuration

Set these as environment variables (see `.env.example`):

| Variable | Default | |
|---|---|---|
| `META_MODEL_API_KEY` | (required) | Meta Model API key |
| `MODEL` | `muse-spark-1.3-contributor` | Agent model |
| `REASONING_EFFORT` | `medium` | `minimal`/`low`/`medium`/`high` |
| `COMPACT_TOKENS` | `120000` | Prompt size at which older conversation is folded into the summary |
| `ADDR` | `:8080` | Listen address |
| `DATA_DIR` | `./data` | State directory |
| `VAULT_KEY_FILE` | `DATA_DIR/vault.key` | Key for saved logins (32 bytes, base64); made if missing |
| `PUBLIC_URL` | | Used in notification links |
| `ALLOW_SIGNUP` | `false` | Open sign-up after the first (admin) user |
| `SECURE_COOKIES` | `false` | Set when serving over HTTPS |
| `SANDBOX_UIDS` | `true` on Linux as root | Per-user UIDs for Chrome and the shell |
| `SHELL_ENABLED` | `true` | Allow the `run_shell` tool |
| `BROWSER_IDLE_MINUTES` | `15` | Close idle browsers to save RAM |
| `BROWSER_HEADLESS` | `true` | |
| `CHROME_PATH` | auto | |
| `MAX_TURNS` | `200` | Model turns per stretch of work |
| `RESEND_API_KEY`, `MAIL_FROM` | | Email notifications |

## Logs

The server logs to stderr and to `DATA_DIR/alexa.log`, which is rotated to `alexa.log.1` past 10 MB. In Docker you can also use `docker compose logs -f`. Each model call, tool call and compaction is logged with its timing:

```
msg="model turn" user=1 turn=0 first_event=1.1s first_output=1.3s total=1.6s in=7344 cached=7153 out=124 reasoning=32 items=8
msg=tool user=1 name=browser_open took=1.393s err=<nil>
msg="compacted session" user=1 folded=49 kept=19 context_was=24938 summary_chars=2418 took=6.4s
msg="model API response headers" total=10.5s reused_conn=true dns=0s connect=0s tls=0s server_wait=10.5s
```

`first_event` is how long the model API took to start answering, and `cached` is how much of the prompt it served from its cache. The `model API response headers` warning appears when the API takes more than 5 s to start answering. It splits the wait into DNS, connect, TLS and time spent waiting on the API (`server_wait`). Start with `-debug` (or `DEBUG=1`) to log every request.

## Architecture

```
cmd/alexagent        entry point, wiring, -create-user
internal/agent       the session loop (Responses API, stateless replay with encrypted
                     reasoning), tools, system prompt, compaction, scheduler
internal/llm         Meta Model API client (streaming SSE, retries)
internal/browser     per-user Chrome (chromedp): DOM indexing, annotated screenshots,
                     actions, popups, screencast, human input for take-over
internal/voice       realtime ASR WebSocket proxy + batch transcription
internal/api         HTTP/WebSocket API, auth (bcrypt, cookie sessions, CSRF header),
                     live view, reports, ICS export, uploads/files, saved logins
internal/vault       saved logins: encryption, matching a page to its site
internal/hub         per-user event fan-out to open clients, and whether one is on screen
internal/store       SQLite (modernc, pure Go, WAL)
internal/notify      Web Push (VAPID) + email
internal/userenv     per-user directories and UIDs
web/                 React UI (embedded into the binary at build time)
```

**How the session works.** Each user's session has four parts, all in SQLite:

- the model conversation, replayed on every call;
- an inbox of messages that arrived while Alexa was busy;
- the feed, which is what the UI shows;
- the reports.

1. A message starts a run. The loop streams a model turn, executes its tool calls and repeats until the model stops calling tools. Each tool call shows as a step with a short status label.
2. Only `say`, `ask` and `present_report` reach the user, plus `request_takeover` and `request_login` when Alexa needs them in the browser. If a run ends with your message unanswered, Alexa gets one reminder to use `say`. After that, its last text is clipped and used as the reply.
3. Messages that arrive mid-run are added before the next turn, each stamped with its local time.
4. The run suspends on a question, a take-over or a sign-in request. Your reply, hand-back or login resumes it.
   A stretch of work gets `MAX_TURNS` model turns. When it uses them all, Alexa gets one more turn to tell you what it did and what's left, and carries on when you say so.
5. To stay within budget:
   - Old screenshots and page dumps are pruned from the replay.
   - Past `COMPACT_TOKENS`, the older part of the conversation is folded into the summary. Compaction also runs when the API reports a context overflow.
6. To keep the prompt cacheable:
   - The instructions don't change between calls. Times travel with the messages.
   - Memories are a snapshot that refreshes only on compaction, a reset or your own edits. Alexa's own `memory_save` doesn't touch it, since the conversation already shows it.
7. What's on screen (nothing, the browser or a report) is session state, pushed to every open client over the WebSocket.

## Tests

```bash
go test ./...            # store, vault, API (session, compaction, auth/CSRF/isolation, sign-in), browser (needs Chrome), agent
cd web && npm run build  # type-check + build
```

The API tests drive the whole session loop against a scripted fake model. They cover:

- the hidden text, the reminder and the fallback;
- refused long messages;
- reports, questions, take-over and stop;
- messages sent mid-run;
- running out of turns, when Alexa says where things stand;
- compaction;
- the memory snapshot;
- signing in: the secure form, saved logins and a login typed into a real page, with the password never reaching the model, the feed or the API;
- isolation between users;
- uploads keeping their names.

The browser tests run a real Chrome against small local pages: page state, frames, dialogs, popups (including one that closes itself), downloads, the pointer's glide, and filling in saved logins.
