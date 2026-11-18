import Lean

open Lean

abbrev AuditM := StateT Environment IO

instance : MonadEnv AuditM where
  getEnv := get
  modifyEnv f := modify f

def allowedAxioms : Array Name := #[``propext, ``Classical.choice, ``Quot.sound]

def main (args : List String) : IO UInt32 := do
  if args.isEmpty then
    IO.eprintln "lean-axiom-audit: pass the project's module names"
    return 2
  initSearchPath (← findSysroot)
  let modules := args.toArray.map String.toName
  let env ← importModules (modules.map fun module => { module }) {}
  let moduleNames := env.allImportedModuleNames
  let mut failures := 0
  let mut audited := 0
  for (name, info) in env.constants.toList do
    let some idx := env.getModuleIdxFor? name | continue
    let some owner := moduleNames[idx.toNat]? | continue
    unless modules.contains owner do continue
    audited := audited + 1
    if info matches .axiomInfo _ then
      IO.eprintln s!"lean-axiom-audit: {owner} declares axiom {name}"
      failures := failures + 1
    let (axioms, _) ← (collectAxioms name : AuditM (Array Name)).run env
    for ax in axioms do
      unless allowedAxioms.contains ax do
        IO.eprintln s!"lean-axiom-audit: {name} depends on {ax}"
        failures := failures + 1
  if audited == 0 then
    IO.eprintln "lean-axiom-audit: no declarations found in the given modules"
    return 1
  IO.println s!"lean-axiom-audit: {audited} declarations, axioms limited to propext, Classical.choice, Quot.sound"
  return if failures == 0 then 0 else 1
