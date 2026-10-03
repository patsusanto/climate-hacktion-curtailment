import { useCallback, useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { fetchStep, streamPlayground } from "./api";
import Chart from "./Chart";
import Decision from "./Decision";
import Loading from "./Loading";
import Payback from "./Payback";
import { Mascot } from "../mascot/Mascot";
import { kwh, money } from "./format";
import { seasonLabel } from "./types";
import type {
  Season,
  PlaygroundMeta,
  PlaygroundRequest,
  PlaygroundSummary,
  PlaygroundTick,
  StepDecision,
} from "./types";
import "./playground.css";

type Status = "idle" | "streaming" | "done" | "stopped" | "error";

/** How long a whole window takes to play at 1×. */
const PLAY_SECONDS = 25;
const RATES = [1, 4, 16] as const;

export default function Playground() {
  const reduce = usePrefersReducedMotion();
  const reduceRef = useRef(reduce);
  reduceRef.current = reduce;

  const [address, setAddress] = useState("1 Harbour Street, Sydney NSW");
  const [pv, setPv] = useState("6.6");
  const [battery, setBattery] = useState("13.5");
  const [batteryKw, setBatteryKw] = useState("");
  const [exportCap, setExportCap] = useState("");
  const [dailyLoad, setDailyLoad] = useState("");
  const [season, setSeason] = useState<Season>("summer");

  const [status, setStatus] = useState<Status>("idle");
  const [formError, setFormError] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [meta, setMeta] = useState<PlaygroundMeta | null>(null);
  const [ticks, setTicks] = useState<PlaygroundTick[]>([]);
  const [latest, setLatest] = useState<PlaygroundTick | null>(null);
  const [count, setCount] = useState(0);
  const [summary, setSummary] = useState<PlaygroundSummary | null>(null);
  const [runId, setRunId] = useState<string | null>(null);
  const [pinned, setPinned] = useState(false);
  const [selected, setSelected] = useState<number | null>(null);
  const [hover, setHover] = useState<number | null>(null);
  const [decision, setDecision] = useState<StepDecision | null>(null);
  const [decisionError, setDecisionError] = useState<string | null>(null);
  const [liveText, setLiveText] = useState("");
  const [paused, setPaused] = useState(false);
  const [rate, setRate] = useState<number>(1);
  const [received, setReceived] = useState(0);
  // The loading screen stays until its stages are all ticked; the playback starts after it.
  const [introDone, setIntroDone] = useState(false);

  const buf = useRef<PlaygroundTick[]>([]);
  const abortRef = useRef<AbortController | null>(null);
  const runSeq = useRef(0);
  const cancelPaint = useRef<() => void>(() => {});
  // Playback: steps arrive as fast as the server has them; the page shows them on its own clock.
  const play = useRef({
    played: 0,
    total: 0,
    rate: 1,
    paused: false,
    painted: -1,
    summary: null as PlaygroundSummary | null,
    pendingTotal: 0, // steps in the run, waiting for the loading screen to finish
  });

  useEffect(() => {
    const id = "playground-fonts";
    if (!document.getElementById(id)) {
      const google = document.createElement("link");
      google.rel = "preconnect";
      google.href = "https://fonts.googleapis.com";
      const gstatic = document.createElement("link");
      gstatic.rel = "preconnect";
      gstatic.href = "https://fonts.gstatic.com";
      gstatic.crossOrigin = "anonymous";
      const css = document.createElement("link");
      css.id = id;
      css.rel = "stylesheet";
      css.href =
        "https://fonts.googleapis.com/css2?family=DM+Sans:opsz,wght@9..40,400&family=Trocchi&display=swap";
      document.head.append(google, gstatic, css);
    }

    const prevTitle = document.title;
    const prevMargin = document.body.style.margin;
    const prevBg = document.body.style.background;
    const prevHtml = document.documentElement.style.background;
    document.title = "Playground";
    document.body.style.margin = "0";
    document.body.style.background = "#f4f0e8";
    document.documentElement.style.background = "#f4f0e8";
    return () => {
      document.title = prevTitle;
      document.body.style.margin = prevMargin;
      document.body.style.background = prevBg;
      document.documentElement.style.background = prevHtml;
      abortRef.current?.abort();
      cancelPaint.current();
    };
  }, []);

  const lastIndex = ticks.length > 0 ? ticks[ticks.length - 1].i : -1;
  const shown = pinned && selected != null ? selected : lastIndex;
  const active = hover ?? (shown >= 0 ? shown : null);
  const focus =
    active != null && ticks[active]?.i === active ? ticks[active] : null;
  const decisionIndex =
    runId && shown >= 0 && (status !== "streaming" || pinned) ? shown : null;

  useEffect(() => {
    setDecision(null);
    setDecisionError(null);
    if (decisionIndex == null || !runId) return;
    const ac = new AbortController();
    const timer = window.setTimeout(() => {
      fetchStep(runId, decisionIndex, ac.signal)
        .then((step) => {
          if (!ac.signal.aborted) setDecision(step);
        })
        .catch((err: unknown) => {
          if (ac.signal.aborted) return;
          setDecisionError(
            err instanceof Error ? err.message : "Could not load this step.",
          );
        });
    }, 150);
    return () => {
      ac.abort();
      window.clearTimeout(timer);
    };
  }, [runId, decisionIndex]);

  function paint(seq: number, upTo: number) {
    if (seq !== runSeq.current || upTo === play.current.painted) return;
    play.current.painted = upTo;
    const all = buf.current.slice(0, upTo);
    const last = all[all.length - 1] ?? null;
    setCount(all.length);
    setLatest(last);
    if (!reduceRef.current) setTicks(all);
    if (last && (all.length === 1 || all.length % 48 === 0)) {
      setLiveText(`Step ${all.length}. ${paidSoFar(last)}.`);
    }
  }

  const finishIntro = useCallback(() => {
    play.current.total = play.current.pendingTotal;
    setIntroDone(true);
  }, []);

  function togglePause() {
    play.current.paused = !play.current.paused;
    setPaused(play.current.paused);
  }

  function changeRate(next: number) {
    play.current.rate = next;
    setRate(next);
    if (play.current.paused) togglePause();
  }

  function skipToEnd() {
    play.current.played = buf.current.length;
  }

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    let request: PlaygroundRequest;
    try {
      request = toRequest({
        address,
        pv,
        battery,
        batteryKw,
        exportCap,
        dailyLoad,
        season,
      });
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Check the form.");
      return;
    }
    await run(request);
  }

  function startScenario(scenario: Scenario) {
    setPv(scenario.pv);
    setBattery(scenario.battery);
    setBatteryKw("");
    setExportCap("");
    setDailyLoad(scenario.dailyLoad);
    setFormError(null);
    void run(
      toRequest({
        address,
        pv: scenario.pv,
        battery: scenario.battery,
        batteryKw: "",
        exportCap: "",
        dailyLoad: scenario.dailyLoad,
        season,
      }),
    );
  }

  async function run(request: PlaygroundRequest) {
    setFormError(null);
    cancelPaint.current();
    const seq = ++runSeq.current;
    abortRef.current?.abort();
    const ac = new AbortController();
    abortRef.current = ac;
    buf.current = [];
    play.current = {
      played: 0,
      total: 0,
      rate: play.current.rate,
      paused: false,
      painted: -1,
      summary: null,
      pendingTotal: 0,
    };
    setPaused(false);
    setIntroDone(false);
    setReceived(0);

    setStatus("streaming");
    setError(null);
    setMeta(null);
    setTicks([]);
    setLatest(null);
    setCount(0);
    setSummary(null);
    setRunId(null);
    setPinned(false);
    setSelected(null);
    setHover(null);
    setDecision(null);
    setDecisionError(null);
    setLiveText("Run started.");

    const p = play.current;
    const finish = () => {
      cancelPaint.current();
      const next = p.summary;
      if (!next) return;
      paint(seq, buf.current.length);
      setTicks(buf.current.slice());
      setSummary(next);
      setStatus("done");
      setLiveText(
        `Finished ${buf.current.length} steps. With self-consumption you would have ${paidPhrase(next.self_consumption.bill_aud, true)}. With this system you ${paidPhrase(next.planner.bill_aud, false)}.`,
      );
    };
    let last = performance.now();
    const timer = window.setInterval(() => {
      if (seq !== runSeq.current) return;
      const now = performance.now();
      const available = buf.current.length;
      if (reduceRef.current) {
        p.played = available;
      } else if (!p.paused && p.total > 0) {
        p.played += (p.total / (PLAY_SECONDS * 1000)) * (now - last) * p.rate;
      }
      last = now;
      p.played = Math.min(p.played, available);
      setReceived(available);
      paint(seq, Math.floor(p.played));
      if (p.summary && Math.floor(p.played) >= available) finish();
    }, 60);
    cancelPaint.current = () => window.clearInterval(timer);
    const flush = () => {
      cancelPaint.current();
      const all = buf.current.slice(0, Math.floor(p.played));
      setCount(all.length);
      setTicks(all);
      setLatest(all[all.length - 1] ?? null);
    };

    try {
      const result = await streamPlayground(
        request,
        {
          onMeta: (next, id) => {
            if (seq !== runSeq.current) return;
            p.pendingTotal = next.window.n; // playback starts when the loading screen hands over
            setMeta(next);
            setRunId(id);
          },
          onTick: (tick) => {
            if (seq !== runSeq.current) return;
            buf.current.push(tick);
          },
          onDone: (next) => {
            if (seq !== runSeq.current) return;
            p.summary = next; // shown when the playback reaches the end
          },
        },
        ac.signal,
      );
      if (seq !== runSeq.current) return;
      if (result === "stopped") {
        flush();
        setStatus("stopped");
        setLiveText("Stopped.");
      }
    } catch (err) {
      if (seq !== runSeq.current) return;
      flush();
      if (ac.signal.aborted) {
        setStatus("stopped");
        setLiveText("Stopped.");
        return;
      }
      setError(err instanceof Error ? err.message : "The stream failed.");
      setStatus("error");
    }
  }

  function stop() {
    abortRef.current?.abort();
    if (status !== "streaming") return;
    // The download may already be complete; stop the playback where it is.
    runSeq.current++;
    cancelPaint.current();
    const all = buf.current.slice(0, Math.floor(play.current.played));
    setCount(all.length);
    setTicks(all);
    setLatest(all[all.length - 1] ?? null);
    setStatus("stopped");
    setLiveText("Stopped.");
  }

  const alert = formError || error;

  return (
    <div className="playground">
      <p className="sr-only" aria-live="polite">
        {liveText}
      </p>
      <header className="mast">
        <a className="back" href="/">
          Home
        </a>
        <div className="brand">
          <Mascot />
          <h1>See what you could have saved</h1>
        </div>
      </header>

      <div className="workspace">
        <aside className="sidebar">
          <form className="card" onSubmit={(event) => void onSubmit(event)}>
            <div className="fields">
              <label className="span-3">
                <span className="eyebrow">Address</span>
                <input
                  value={address}
                  onChange={(event) => setAddress(event.target.value)}
                  autoComplete="street-address"
                  required
                />
              </label>
              <label>
                <span className="eyebrow">Roof kW</span>
                <input
                  type="number"
                  min="0"
                  step="any"
                  value={pv}
                  onChange={(event) => setPv(event.target.value)}
                  required
                />
              </label>
              <label>
                <span className="eyebrow">Battery kWh</span>
                <input
                  type="number"
                  min="0"
                  step="any"
                  value={battery}
                  onChange={(event) => setBattery(event.target.value)}
                  required
                />
              </label>
              <label>
                <span className="eyebrow">Battery kW</span>
                <input
                  type="number"
                  min="0"
                  step="any"
                  placeholder="5"
                  value={batteryKw}
                  onChange={(event) => setBatteryKw(event.target.value)}
                />
              </label>
              <label>
                <span className="eyebrow">Export cap kW</span>
                <input
                  type="number"
                  min="0"
                  step="any"
                  placeholder="5"
                  value={exportCap}
                  onChange={(event) => setExportCap(event.target.value)}
                />
              </label>
              <label>
                <span className="eyebrow">Daily load kWh</span>
                <input
                  type="number"
                  min="0"
                  step="any"
                  placeholder="18"
                  value={dailyLoad}
                  onChange={(event) => setDailyLoad(event.target.value)}
                />
              </label>
              <fieldset className="span-3 season">
                <legend className="eyebrow">Season</legend>
                <div className="season-options">
                  {(Object.keys(seasonLabel) as Season[]).map((s) => (
                    <label
                      key={s}
                      className={
                        season === s ? "season-option on" : "season-option"
                      }
                    >
                      <input
                        type="radio"
                        name="season"
                        value={s}
                        checked={season === s}
                        onChange={() => setSeason(s)}
                      />
                      {seasonLabel[s]}
                    </label>
                  ))}
                </div>
              </fieldset>
              <div className="span-3 actions">
                <button
                  className="primary"
                  type="submit"
                  disabled={status === "streaming"}
                >
                  {status === "streaming" ? "Running" : "Run"}
                </button>
                {status === "streaming" && (
                  <button className="stop" type="button" onClick={stop}>
                    Stop
                  </button>
                )}
              </div>
            </div>
            {alert && (
              <p className="alert" role="alert">
                {alert}
              </p>
            )}
          </form>
        </aside>

        <div className="stage">
          {status === "idle" && !meta && (
            <div className="scenarios">
              <p className="note">
                Look at the results from one of our precomputed scenarios
              </p>
              <div className="scenario-list">
                {scenarios.map((scenario) => (
                  <button
                    key={scenario.id}
                    type="button"
                    className="scenario"
                    onClick={() => startScenario(scenario)}
                  >
                    <img className="scenario-art" src={scenario.image} alt="" />
                    <span className="scenario-name">{scenario.name}</span>
                    <span className="note">{scenario.detail}</span>
                  </button>
                ))}
              </div>
            </div>
          )}
          {status === "streaming" && !introDone && !error && (
            <Loading
              reduce={reduce}
              ready={meta != null}
              onFinished={finishIntro}
            />
          )}

          {meta && (introDone || status !== "streaming") && (
            <section className="chart-section">
              <div className="card chart-card">
                {reduce && status === "streaming" && (
                  <p className="note">
                    Collecting steps. The chart will appear when the run
                    finishes.
                  </p>
                )}
                <Chart
                  ticks={reduce && status === "streaming" ? [] : ticks}
                  total={meta.window.n}
                  start={meta.window.start}
                  stepMinutes={meta.window.step_minutes}
                  active={reduce && status === "streaming" ? null : active}
                  waiting={status === "streaming" && count === 0}
                  onHover={setHover}
                  onPick={(i) => {
                    setPinned(true);
                    setSelected(i);
                  }}
                />
                <Comparison
                  selfEnergy={latest ? latest.cumulative_self_aud : null}
                  savings={latest ? latest.cumulative_savings_aud : null}
                  supply={latest ? latest.cumulative_supply_aud : null}
                  summary={summary}
                />
                {summary?.warnings && summary.warnings.length > 0 && (
                  <div className="warnings" role="note">
                    {summary.warnings.map((warning) => (
                      <p key={warning}>{warning}</p>
                    ))}
                  </div>
                )}
                {status === "streaming" && !reduce && (
                  <div className="playback" role="group" aria-label="Playback">
                    <button
                      type="button"
                      className="follow"
                      onClick={togglePause}
                    >
                      {paused ? "Play" : "Pause"}
                    </button>
                    {RATES.map((r) => (
                      <button
                        key={r}
                        type="button"
                        className={rate === r ? "follow on" : "follow"}
                        aria-pressed={rate === r}
                        onClick={() => changeRate(r)}
                      >
                        {r}×
                      </button>
                    ))}
                    <button
                      type="button"
                      className="follow"
                      onClick={skipToEnd}
                      disabled={received === 0}
                    >
                      Skip to end
                    </button>
                    {received < meta.window.n && (
                      <span className="note">
                        Computing,{" "}
                        {Math.round((received / meta.window.n) * 100)}% ready
                      </span>
                    )}
                  </div>
                )}
                {ticks.length > 1 && (
                  <div className="scrub-row">
                    <label className="scrub">
                      <span className="eyebrow">
                        Step {shown + 1} of{" "}
                        {meta.window.n.toLocaleString("en-AU")}
                      </span>
                      <input
                        type="range"
                        min={0}
                        max={lastIndex}
                        value={Math.min(shown, lastIndex)}
                        aria-valuetext={
                          focus
                            ? `${focus.t} ${focus.action} ${money(focus.cumulative_savings_aud)}`
                            : undefined
                        }
                        onChange={(event) => {
                          setPinned(true);
                          setSelected(Number(event.target.value));
                        }}
                      />
                    </label>
                    {pinned && status === "streaming" && (
                      <button
                        type="button"
                        className="follow"
                        onClick={() => setPinned(false)}
                      >
                        Follow live
                      </button>
                    )}
                  </div>
                )}
                <div
                  className="progress"
                  role="progressbar"
                  aria-valuemin={0}
                  aria-valuemax={meta.window.n}
                  aria-valuenow={count}
                >
                  <span
                    className="ready"
                    style={{
                      width: `${Math.min(100, (Math.max(received, count) / meta.window.n) * 100)}%`,
                    }}
                  />
                  <span
                    style={{
                      width: `${Math.min(100, (count / meta.window.n) * 100)}%`,
                    }}
                  />
                </div>
                {focus && (
                  <Decision
                    tick={focus}
                    decision={decision}
                    loading={
                      decisionIndex != null && !decision && !decisionError
                    }
                    error={decisionError}
                  />
                )}
              </div>
              {summary?.payback && (
                <Payback payback={summary.payback} spec={meta.spec} />
              )}
              <p className="note assumptions">{meta.assumptions.note}</p>
              {meta.assumptions.tariff && (
                <p className="note assumptions">{meta.assumptions.tariff}</p>
              )}
              {status === "stopped" && (
                <p className="note">
                  Stopped. These are the steps that had arrived.
                </p>
              )}
            </section>
          )}
        </div>
      </div>
    </div>
  );
}

type Scenario = {
  id: string;
  name: string;
  detail: string;
  image: string;
  pv: string;
  battery: string;
  dailyLoad: string;
};

const scenarios: Scenario[] = [
  {
    id: "unit",
    name: "Small roof",
    detail: "3.3 kW roof, 6.5 kWh battery",
    image: "/small-roof.png",
    pv: "3.3",
    battery: "6.5",
    dailyLoad: "12",
  },
  {
    id: "harbour",
    name: "Family home",
    detail: "6.6 kW roof, 13.5 kWh battery",
    image: "/family-home.png",
    pv: "6.6",
    battery: "13.5",
    dailyLoad: "18",
  },
  {
    id: "large",
    name: "All-electric house",
    detail: "10 kW roof, 27 kWh battery",
    image: "/big-system.png",
    pv: "10",
    battery: "27",
    dailyLoad: "30",
  },
];

function Comparison({
  selfEnergy,
  savings,
  supply,
  summary,
}: {
  selfEnergy: number | null;
  savings: number | null;
  supply: number | null;
  summary: PlaygroundSummary | null;
}) {
  const supplyAud = summary?.supply_aud ?? supply;
  const selfPaid =
    summary != null
      ? summary.self_consumption.bill_aud
      : selfEnergy != null
        ? selfEnergy + (supplyAud ?? 0)
        : null;
  const systemEnergy =
    selfEnergy != null && savings != null ? selfEnergy - savings : null;
  const systemPaid =
    summary != null
      ? summary.planner.bill_aud
      : systemEnergy != null
        ? systemEnergy + (supplyAud ?? 0)
        : null;
  // Both running totals include battery wear as well as energy and supply.
  const supplyLine =
    supplyAud != null
      ? ` Includes supply of ${money(supplyAud)} and battery wear.`
      : "";
  return (
    <div className="compare">
      <PaidCard
        eyebrow="Self-consumption"
        paid={selfPaid}
        would
        note={`Battery on its default setting: stores spare solar, runs the house at night.${supplyLine}`}
        bill={summary?.self_consumption}
      />
      <PaidCard
        eyebrow="With this system"
        paid={systemPaid}
        would={false}
        note={`Instead.${supplyLine}`}
        bill={summary?.planner}
      />
    </div>
  );
}

function PaidCard({
  eyebrow,
  paid,
  would,
  note,
  bill,
}: {
  eyebrow: string;
  paid: number | null;
  would: boolean;
  note: string;
  bill?: PlaygroundSummary["self_consumption"];
}) {
  const credit = paid != null && paid < 0;
  return (
    <article className={credit ? "card compare-gain" : "card compare-loss"}>
      <p className="eyebrow">{eyebrow}</p>
      <p className="compare-kicker">
        {paid == null
          ? "Bill"
          : would
            ? credit
              ? "You would have been paid"
              : "You would have paid"
            : credit
              ? "You were paid"
              : "You paid"}
      </p>
      <p className="compare-figure">
        {paid == null ? "—" : credit ? `+${money(-paid)}` : money(paid)}
      </p>
      <p className="note">{note}</p>
      {bill && <BillFacts bill={bill} />}
    </article>
  );
}

function paidPhrase(amount: number, would: boolean): string {
  const credit = amount < 0;
  const figure = credit ? `+${money(-amount)}` : money(amount);
  if (would) return credit ? `been paid ${figure}` : `paid ${figure}`;
  return credit ? `were paid ${figure}` : `paid ${figure}`;
}

function paidSoFar(tick: PlaygroundTick): string {
  const amount =
    tick.cumulative_self_aud -
    tick.cumulative_savings_aud +
    tick.cumulative_supply_aud;
  return `With this system you ${paidPhrase(amount, false)} so far`;
}

function BillFacts({ bill }: { bill: PlaygroundSummary["self_consumption"] }) {
  return (
    <dl className="facts">
      <div>
        <dt>Import</dt>
        <dd>{kwh(bill.grid_import_kwh)}</dd>
      </div>
      <div>
        <dt>Export</dt>
        <dd>{kwh(bill.grid_export_kwh)}</dd>
      </div>
      <div>
        <dt>Clipped</dt>
        <dd>{kwh(bill.clipped_kwh)}</dd>
      </div>
      <div>
        <dt>Battery throughput</dt>
        <dd>{kwh(bill.throughput_ac_kwh)}</dd>
      </div>
      {bill.wear_aud != null && (
        <div>
          <dt>Battery wear</dt>
          <dd>{money(bill.wear_aud)}</dd>
        </div>
      )}
    </dl>
  );
}

function usePrefersReducedMotion(): boolean {
  const [on, setOn] = useState(
    () =>
      typeof window !== "undefined" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches,
  );
  useEffect(() => {
    const media = window.matchMedia("(prefers-reduced-motion: reduce)");
    const apply = () => setOn(media.matches);
    apply();
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, []);
  return on;
}

function toRequest(input: {
  address: string;
  pv: string;
  battery: string;
  batteryKw: string;
  exportCap: string;
  dailyLoad: string;
  season: Season;
}): PlaygroundRequest {
  const address = input.address.trim();
  if (!address) throw new Error("Add an address.");
  const pvKw = requiredNumber("Roof kW", input.pv);
  const batteryKwh = requiredNumber("Battery kWh", input.battery);
  const request: PlaygroundRequest = {
    address,
    pv_kw_ac: pvKw,
    battery_kwh: batteryKwh,
    window: input.season,
  };
  const power = optionalNumber("Battery kW", input.batteryKw, false);
  const cap = optionalNumber("Export cap", input.exportCap, true);
  const load = optionalNumber("Daily load", input.dailyLoad, false);
  if (power != null) request.battery_kw = power;
  if (cap != null) request.export_cap_kw = cap;
  if (load != null) request.daily_load_kwh = load;
  return request;
}

function requiredNumber(label: string, raw: string): number {
  const n = Number(raw);
  if (!Number.isFinite(n) || n <= 0)
    throw new Error(`${label} needs to be greater than zero.`);
  return n;
}

function optionalNumber(
  label: string,
  raw: string,
  allowZero: boolean,
): number | undefined {
  if (raw.trim() === "") return undefined;
  const n = Number(raw);
  if (!Number.isFinite(n) || (allowZero ? n < 0 : n <= 0)) {
    throw new Error(
      allowZero
        ? `${label} needs to be zero or greater, or left empty.`
        : `${label} needs to be greater than zero, or left empty.`,
    );
  }
  return n;
}
