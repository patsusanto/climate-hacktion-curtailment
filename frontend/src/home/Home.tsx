import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { Mascot } from "../mascot/Mascot";
import { actionFill, actionLabel } from "../playground/actions";
import type { Action } from "../playground/types";
import "./home.css";

const houses = [
  {
    name: "Small roof",
    detail: "3.3 kW roof, 6.5 kWh battery",
    image: "/small-roof.png",
    self: 50.07,
    system: 40.38,
    gap: 9.69,
  },
  {
    name: "Family home",
    detail: "6.6 kW roof, 13.5 kWh battery",
    image: "/family-home.png",
    self: 43.27,
    system: 28.63,
    gap: 14.64,
  },
  {
    name: "All-electric house",
    detail: "10 kW roof, 27 kWh battery",
    image: "/big-system.png",
    self: 48.84,
    system: 30.99,
    gap: 17.85,
  },
];

const beats: Array<{ price: number; action: Action }> = [
  { price: -18, action: "charge" },
  { price: 42, action: "charge_surplus" },
  { price: 96, action: "discharge_load" },
  { price: 186, action: "discharge" },
  { price: 70, action: "hold" },
];

export default function Home() {
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
    document.title = "Cubert.AI";
    document.body.style.margin = "0";
    document.body.style.background = "#f4f0e8";
    document.documentElement.style.background = "#f4f0e8";
    return () => {
      document.title = prevTitle;
      document.body.style.margin = prevMargin;
      document.body.style.background = prevBg;
      document.documentElement.style.background = prevHtml;
    };
  }, []);

  return (
    <div className="home">
      <a className="skip" href="#problem">
        Skip to the story
      </a>
      <header className="nav">
        <a className="brand" href="#top">
          <span aria-hidden="true">
            <Mascot size={40} />
          </span>
          <span>Cubert.AI</span>
        </a>
        <nav className="nav-links" aria-label="Page">
          <a href="#problem">Problem</a>
          <a href="#outcomes">Outcomes</a>
          <a href="#electrification">Electrification</a>
          <a href="#how">How it works</a>
          <a href="#results">Results</a>
          <a href="/playground">Playground</a>
        </nav>
      </header>

      <main id="top">
        <section className="hero">
          <div className="hero-copy">
            <p className="eyebrow">Electrification</p>
            <h1>Low returns stall the next roof.</h1>
            <p className="lede">
              People hesitate to buy solar and a battery when the payback is
              modest. A large part of that is timing: the system sells when the
              price is weakest. A forecast, and a plan for the house, is how the
              same equipment earns more.
            </p>
            <div className="hero-actions">
              <a className="primary" href="/playground">
                Open the playground
              </a>
              <a className="ghost" href="#how">
                How it works
              </a>
            </div>
          </div>
          <div className="hero-mark">
            <Mascot size={368} />
          </div>
        </section>

        <Reveal id="problem">
          <p className="eyebrow">The problem</p>
          <h2>The system sells at the wrong hour.</h2>
          <p>
            A roof that exports as soon as the sun is out meets the market at
            its cheapest, alongside every other roof. By evening, when the price
            lifts, the chance to sell has gone. The return stays thin, and the
            next solar or battery purchase is harder to justify.
          </p>
          <div className="plan">
            <article className="card">
              <p className="eyebrow">Forecast</p>
              <h3>See the price before it arrives.</h3>
              <p>
                The model looks ahead at the NSW1 price: a low, middle and high
                case, built from recent prices, the weather, and the market’s
                own pre-dispatch. The expensive hours are visible before they
                get here.
              </p>
            </article>
            <article className="card">
              <p className="eyebrow">Plan</p>
              <h3>The best next eight hours for this house.</h3>
              <p>
                Every five minutes the decision engine builds a plan on that
                middle forecast. It chooses when this roof should charge, when
                it should hold, and when it should export.
              </p>
            </article>
            <article className="card">
              <p className="eyebrow">Command</p>
              <h3>Sell when the power is worth more.</h3>
              <p>
                The first five minutes of the plan become a command to the
                battery and the inverter. Surplus is kept while the price is
                weak, and sold when the forecast says the price will pay.
              </p>
            </article>
          </div>
        </Reveal>

        <Reveal id="outcomes" className="outcomes">
          <p className="eyebrow">Example outcome</p>
          <h2>Sooner to breakeven.</h2>
          <p>
            One house in the replay: a 10 kW roof, a 27 kWh battery, about 30
            kWh a day. From 16 July to 18 August 2026 the battery followed the
            price instead of selling with the midday sun. The extra return is
            what pays the original purchase down.
          </p>
          <div className="figures">
            <article className="figure">
              <p className="figure-value">36.5%</p>
              <p className="figure-note">
                lower bill after optimization. $48.84 with self-consumption,
                $30.99 with the plan.
              </p>
            </article>
            <article className="figure">
              <p className="figure-value">$192</p>
              <p className="figure-note">
                a year sooner toward a return, so the roof and battery take less
                time to pay back.
              </p>
            </article>
          </div>
        </Reveal>

        <Reveal id="electrification" className="electrification">
          <p className="eyebrow">Focus area · Electrification</p>
          <h2>A profitable roof is an easier roof to buy.</h2>
          <p>
            When solar earns more, owning it is more attractive. More households
            adopt it. Electrification rates rise with them: heat, cooking, and
            cars move onto electricity the house can pay for.
          </p>
          <div className="chain">
            <article className="chain-step">
              <img src="/profit.png" alt="" />
              <h3>More profit</h3>
              <p>The same roof returns more once it sells at the right hour.</p>
            </article>
            <article className="chain-step">
              <img src="/adoption.png" alt="" />
              <h3>More adoption</h3>
              <p>
                A clearer return makes the next solar and battery purchase
                easier to choose.
              </p>
            </article>
            <article className="chain-step">
              <img src="/electrification.png" alt="" />
              <h3>More electrification</h3>
              <p>
                Heat, cooking, and cars follow, because the supply can pay its
                way.
              </p>
            </article>
          </div>
        </Reveal>

        <Reveal id="compare">
          <p className="eyebrow">The existing offer</p>
          <h2>A virtual power plant takes the battery.</h2>
          <p>
            An operator dispatches the battery and pays the house a share of a
            pool. Households object on four counts. This platform is built so
            those objections do not apply.
          </p>
          <div className="table-wrap">
            <table className="offer">
              <thead>
                <tr>
                  <th scope="col">Complaint</th>
                  <th scope="col">A virtual power plant</th>
                  <th scope="col">This platform</th>
                </tr>
              </thead>
              <tbody>
                <tr>
                  <th scope="row">Transparency</th>
                  <td>
                    The payment is a share of a pool, and it is hard to check
                    against the price of that hour.
                  </td>
                  <td>
                    Every five minutes shows the price, the estimate, the
                    command, and the running bill.
                  </td>
                </tr>
                <tr>
                  <th scope="row">Control</th>
                  <td>
                    The operator decides when the battery charges and when it
                    sells. The household loses that say.
                  </td>
                  <td>
                    The household keeps the battery. The plan is for this house,
                    and the command is the one on the page.
                  </td>
                </tr>
                <tr>
                  <th scope="row">Retail plan</th>
                  <td>
                    Some programs require the operator’s own electricity plan.
                    That plan can cost more than the credit is worth.
                  </td>
                  <td>
                    No plan switch. The household stays on its own tariff, and
                    the saving is the gap against self-consumption.
                  </td>
                </tr>
                <tr>
                  <th scope="row">Hardware</th>
                  <td>
                    Enrolment is limited to the batteries and inverters the
                    operator can control, so the household may have to buy that
                    kit.
                  </td>
                  <td>
                    The plan is sized to the roof and the battery already
                    installed. Joining does not mean replacing them.
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </Reveal>

        <Pipeline />

        <Reveal id="try">
          <p className="eyebrow">Try a house</p>
          <h2>Look at one of the three roofs.</h2>
          <p className="lede slim">
            The playground replays 16 July to 18 August 2026 and draws both
            bills as the steps arrive.
          </p>
          <div className="houses">
            {houses.map((house) => (
              <a key={house.name} className="house" href="/playground">
                <img src={house.image} alt="" />
                <span className="house-name">{house.name}</span>
                <span className="note">{house.detail}</span>
                <span className="house-gap">Gap {aud(house.gap)}</span>
              </a>
            ))}
          </div>
        </Reveal>
      </main>

      <footer className="foot">
        <span aria-hidden="true">
          <Mascot size={56} />
        </span>
        <p>
          NSW1 only. The household shape is synthetic, built on real weather.
          Prices and pre-dispatch come from AEMO. Weather comes from Open-Meteo.
          Forecasts and the planner run in Go. This page is React.
        </p>
      </footer>
    </div>
  );
}

function Reveal({
  id,
  className,
  children,
}: {
  id: string;
  className?: string;
  children: ReactNode;
}) {
  const [ref, shown] = useShown<HTMLElement>();
  const classes = ["reveal", className, shown ? "in" : ""]
    .filter(Boolean)
    .join(" ");
  return (
    <section id={id} ref={ref} className={classes}>
      {children}
    </section>
  );
}

function Pipeline() {
  const [ref, shown] = useShown<HTMLElement>();
  return (
    <section id="how" ref={ref} className={shown ? "reveal in" : "reveal"}>
      <p className="eyebrow">What goes in</p>
      <h2>Weather and price, then a command.</h2>
      <div className={shown ? "pipeline playing" : "pipeline"}>
        <div className="rail" aria-hidden="true" />
        <article className="station">
          <span className="dot">1</span>
          <h3>Weather</h3>
          <p>
            Day-ahead forecasts for Sydney, Dubbo and Goulburn, and the weather
            observed on the roof.
          </p>
        </article>
        <article className="station">
          <span className="dot">2</span>
          <h3>Forecasts</h3>
          <p>
            A price model gives a low, middle and high case for the NSW1 price,
            from recent prices, AEMO pre-dispatch, and that weather. A second
            model forecasts the roof and the household load.
          </p>
        </article>
        <article className="station">
          <span className="dot">3</span>
          <h3>Decision engine</h3>
          <p>
            Every five minutes it plans the cheapest next eight hours on the
            middle price, and carries out the first five minutes.
          </p>
        </article>
        <article className="station">
          <span className="dot">4</span>
          <h3>Commands</h3>
          <p>
            Hold, charge from surplus, charge from the grid, cover the house, or
            export. The command goes to the battery and the inverter.
          </p>
        </article>
      </div>
      <Beat active={shown} />
    </section>
  );
}

function Beat({ active }: { active: boolean }) {
  const [index, setIndex] = useState(0);
  useEffect(() => {
    if (!active || reducedMotion()) return;
    const timer = window.setInterval(
      () => setIndex((value) => (value + 1) % beats.length),
      1600,
    );
    return () => window.clearInterval(timer);
  }, [active]);
  const beat = beats[index];
  const price = beat.price < 0 ? `−$${Math.abs(beat.price)}` : `$${beat.price}`;
  return (
    <p className="beat" aria-hidden="true">
      <span className="eyebrow">One step</span>
      <span className="beat-price">P50 {price}/MWh</span>
      <span
        key={beat.action}
        className="chip"
        style={{ background: actionFill[beat.action] }}
      >
        {actionLabel[beat.action]}
      </span>
    </p>
  );
}

function useShown<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [shown, setShown] = useState(reducedMotion());
  useEffect(() => {
    const el = ref.current;
    if (!el || shown) return;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (!entry?.isIntersecting) return;
        setShown(true);
        observer.disconnect();
      },
      { threshold: 0 },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [shown]);
  return [ref, shown] as const;
}

function reducedMotion() {
  return (
    typeof window !== "undefined" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

function aud(n: number) {
  return n.toLocaleString("en-AU", {
    style: "currency",
    currency: "AUD",
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}
