# 🚴 Cycling Training Context — Athlete Profile

> This file is the athlete profile used as the system prompt when calling
> the Claude API for report and plan generation. It lives in the data volume
> at /data/athlete-profile.md and can be updated via Telegram (/profile set),
> the web API (POST /api/profile), or direct file edit.
>
> Structured values (FTP, zones, weight) are extracted from this file and
> stored in the athlete_config DB table. The analysis engine reads from the
> DB, not from this file directly.

---

## 👤 Athlete Profile
- **Goal:** Build aerobic durability, improve threshold climbing power, manage weight for Stelvio climb (May)
- **FTP:** 251 W *(no recent test; likely slightly underestimating current fitness but kept for consistency)*
- **Weight:** ~90–91 kg *(recent trend slightly ↑ post-travel)*
- **W/kg:** ~2.75–2.80  
- **HRmax:** 184 bpm  
- **Preferred cadence:**  
  - Indoor: 68–75 rpm *(best HR stability)*  
  - Outdoor: ~85 rpm  

---

## ❤️ Heart Rate Zones
| Zone | Range (bpm) | Purpose |
|------|-------------|--------|
| Z1 | <110 | Recovery |
| Z2 | 110–127 | Endurance |
| Z3 | 128–145 | Tempo |
| Z4 | 146–164 | Threshold |
| Z5 | ≥165 | VO₂ |

---

## ⚙️ Power Zones (FTP = 251 W)
| Zone | Power (W) | Purpose |
|------|-----------|--------|
| Z1 | <138 | Recovery |
| Z2 | 139–188 | Endurance |
| Z3 | 189–226 | Tempo / Sweet Spot |
| Z4 | 227–263 | Threshold |
| Z5 | 264–301 | VO₂ |

---

## 🧠 Athlete Characteristics & Constraints

### Physiological Traits
- **Diesel-type athlete**
  - Strong steady-state performance
  - Needs gradual load progression (interval jumps cause failure)
- **Low cadence improves efficiency**
  - 68–72 rpm = best HR/power coupling
- **HR is sensitive to:**
  - hydration
  - stress
  - sleep
  - illness recovery
- **HR drift (decoupling) is primary KPI**

---

### Training Preferences & Constraints
- Weekday sessions: **≤ 60 minutes**
- Preferred rest day: **Friday**
- Long endurance sessions on weekends
- Mostly indoor (Wahoo)
- Frequent travel blocks interrupt consistency

---

## 📊 Data Handling

### Analysis Requirements
Claude should compute:
- duration, avg/max HR
- HR zone distribution
- power distribution
- HR drift (decoupling)
- TRIMP (HR-based approximation)

### Minimal User Input
User prefers low-friction workflow:
- Weekly upload of raw files
- Optional notes:
  - general feeling
  - missed sessions
  - illness / stress

---

## 📈 Key Observations

### 1️⃣ Aerobic Base Progression
- Z2 power progression:
  - ~130W → ~135–140W → **~140W stable**
- HR at same power decreased over time
- Aerobic coupling improved significantly in late Feb

---

### 2️⃣ Post-Illness Recovery (Jan → Feb)
- January: reduced Z2 ceiling (~125–130W), unstable HR
- February: progressive rebuild → **full recovery achieved**
- Early March: occasional HR drift spikes → situational (not regression)

---

### 3️⃣ HR Drift Trends
- Early Feb: moderate drift → rebuilding durability
- Late Feb: **very low drift (<5%) → strong aerobic base**
- Early Mar: isolated spikes (10–13%) → linked to:
  - hydration
  - fatigue
  - travel/stress

---

### 4️⃣ Intensity Response
- Tempo (150–155W): well tolerated
- Sweet Spot: historically strong but requires careful ramp
- Threshold: fragile → must build progressively
- Best progression path:
  - Z2 → Tempo → Sweet Spot → Threshold

---

### 5️⃣ Cadence Efficiency Insight
- 68–72 rpm = optimal HR control
- Higher cadence increases HR disproportionately
- Low cadence should be maintained for endurance + tempo

---

## 🧾 Recent Sessions (Late Feb – Early Mar)

### Summary
- **Sessions:** ~10–12  
- **Weekly volume:** ~6–7 hours  
- **Primary intensity:** Z2 dominant  
- **Tempo introduced:** 1–2 sessions/week  

---

### Key Sessions
| Date | Duration | Power | HR | Drift | Notes |
|------|----------|-------|----|-------|------|
| Feb 28 | ~90 min | ~140W | ~120 bpm | ~1–2% | Excellent coupling |
| Mar 1 | ~90 min | ~140W | ~120 bpm | low | Strong aerobic stability |
| Mar 2 | ~60 min | ~140W | ↑ HR | ~10%+ | Drift spike (likely hydration/stress) |
| Mar 3 | ~60 min | ~135W | ↑ HR | ~10–13% | Repeat spike |
| Jan 27 | ~60 min | ~138W avg | 121 bpm | low | Tempo intro successful |
| Feb 1 | ~100 min | ~139W | 119 bpm | low | Best endurance signal |

---

### Interpretation
- Aerobic base is **solid and improving**
- Drift spikes are **external (not physiological regression)**
- Tempo introduction **successful without fatigue cost**

---

## ⚠️ Flags / Notes

- HR drift spikes (>8–10%) → usually:
  - hydration issue
  - stress / fatigue
  - environmental factors (heat, airflow)

- HR high at low power:
  - early warning of fatigue or illness

- Threshold failures:
  - often due to progression error, not fitness loss

- Weight slightly elevated (~90–91 kg):
  - likely travel + reduced activity
  - managed via consistency, not restriction

---

## 🎯 Training Philosophy

### Core Principles
1. **HR over power**
2. **Repeatability over intensity**
3. **Finish sessions fresh**
4. **Z2 dominance (70–80%)**
5. **Progress gradually**

---

## 🧩 Weekly Structure Template

| Day | Focus |
|-----|------|
| Mon | Recovery / light Z2 |
| Tue | Tempo / structured |
| Wed | Z2 |
| Thu | Z2 or light tempo |
| Fri | Rest |
| Sat | Long endurance |
| Sun | Long endurance (+ optional tempo) |

---

## 🧠 Planning Logic

### Progression Rules
- Increase duration → then intensity
- Extend tempo before introducing Sweet Spot
- Introduce Sweet Spot when:
  - Z2 stable at **~140W**
  - drift consistently <5%

---

## 📅 Current Phase

**Status:**  
- Aerobic base rebuilt after illness  
- Z2 stable at **135–140W**  
- Tempo reintroduced successfully  
- HR stability good with occasional variability  

**Completed:**  
- Recovery phase (Jan)  
- Base consolidation (Feb)  
- Tempo reintroduction (early Mar)  

**Next:**  
- Stabilize 140W as default Z2  
- Extend tempo duration (2×10 → 2×12+)  
- Introduce Sweet Spot (short intervals) after stability  

---

## 🧭 Long-Term Direction
- Improve aerobic durability for long climbs  
- Increase sustainable power (FTP)  
- Reduce HR drift across all intensities  
- Manage weight through consistency  

---

## ✅ Expected Output from Claude

### Analysis
- Weekly report (Markdown):
  - session summaries
  - HR/power trends
  - drift analysis
  - load/TRIMP
  - insights

### Planning
- Weekly plan with:
  - clear watt targets
  - cadence guidance
  - HR caps
  - minimal instructions

---

## 🧠 Key Coaching Insight

> The athlete responds best to **consistent aerobic work + gradual progression**,  
> not aggressive intensity increases.

---

## 🔄 Last Updated
**2026-03-30**

- Aerobic base strong; Z2 stabilized around 135–140W  
- Entering structured build phase with tempo progressing toward Sweet Spot
