# Experiment Tracking

This directory stores immutable snapshots of experiment outputs so each run can
be tracked with Git and pushed to GitHub.

## Layout

- `runs/<timestamp>-<name>/artifacts/`: copied CSV, PDF, and related outputs
- `runs/<timestamp>-<name>/metadata.json`: git branch, commit, command, notes
- `runs/<timestamp>-<name>/README.md`: human-readable run summary

## Recommended Workflow

1. Run the simulation and generate plots.
2. Archive the outputs into a new run directory.
3. Commit that run directory.
4. Push to GitHub.

Example:

```powershell
python .\archive_experiment.py --name paper-monopoly-seed42 --command "go run . -g -N 4 -S 2 -m 4 --exchange_mode limit300 --fee_optimizer paper_monopoly --sim_seed 42"
git add .\experiments\runs\
git commit -m "exp: paper_monopoly seed42"
git push
```

Keep run directories append-only. If you rerun an experiment, archive it as a
new run instead of overwriting an older snapshot.
