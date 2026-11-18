import Init

namespace BoundedQueue

structure Queue (α : Type) (capacity : Nat) where
  items : Nat → α
  head : Nat
  size : Nat

def newBoundedQueue (zero : α) (capacity : Nat) : Queue α capacity :=
  ⟨fun _ => zero, 0, 0⟩

def slot (head offset capacity : Nat) := (head + offset) % capacity

def push (q : Queue α capacity) (value : α) : Queue α capacity × Bool :=
  if q.size = capacity then (q, false) else
    let index := slot q.head q.size capacity
    (⟨fun i => if i = index then value else q.items i, q.head, q.size + 1⟩, true)

def pop (zero : α) (q : Queue α capacity) : Queue α capacity × (α × Bool) :=
  if q.size = 0 then (q, zero, false) else
    (⟨fun i => if i = q.head then zero else q.items i,
      (q.head + 1) % capacity, q.size - 1⟩, q.items q.head, true)

structure Represents (q : Queue α capacity) (fifo : List α) : Prop where
  headBound : q.head < capacity ∨ (capacity = 0 ∧ q.head = 0)
  sizeBound : q.size ≤ capacity
  lengthEq : q.size = fifo.length
  contents : ∀ i (h : i < fifo.length), q.items (slot q.head i capacity) = fifo[i]

theorem emptyRepresents (zero : α) (capacity : Nat) :
    Represents (newBoundedQueue zero capacity) [] := by
  constructor <;> simp [newBoundedQueue] <;> omega

private theorem slotFormula (hh : head < capacity) (hi : i < capacity) :
    slot head i capacity = if head + i < capacity then head + i else head + i - capacity := by
  unfold slot
  split
  · exact Nat.mod_eq_of_lt ‹_›
  · rw [Nat.mod_eq_sub_mod (by omega), Nat.mod_eq_of_lt (by omega)]

private theorem slotNe (hh : head < capacity) (hi : i < capacity) (hj : j < capacity)
    (hne : i ≠ j) : slot head i capacity ≠ slot head j capacity := by
  rw [slotFormula hh hi, slotFormula hh hj]
  split <;> split <;> omega

def fifoPush (capacity : Nat) (fifo : List α) (v : α) : List α × Bool :=
  if fifo.length < capacity then (fifo ++ [v], true) else (fifo, false)

def fifoPop (zero : α) : List α → List α × (α × Bool)
  | [] => ([], zero, false)
  | x :: xs => (xs, x, true)

theorem pushRefines {q : Queue α c} (r : Represents q xs) (v : α) :
    Represents (push q v).1 (fifoPush c xs v).1 ∧ (push q v).2 = (fifoPush c xs v).2 := by
  obtain ⟨headBound, sizeBound, lengthEq, contents⟩ := r
  by_cases full : q.size = c
  · have h : ¬ xs.length < c := by omega
    simpa [push, fifoPush, full, h] using
      (show Represents q xs from ⟨headBound, sizeBound, lengthEq, contents⟩)
  · have room : xs.length < c := by omega
    have headLt : q.head < c := by omega
    simp only [push, full, ite_false, fifoPush, room, ite_true, and_true]
    constructor
    · exact headBound
    · dsimp; omega
    · simp [lengthEq]
    · intro i hi
      dsimp
      by_cases before : i < xs.length
      · rw [ite_eq_right (slotNe headLt (by omega) (by omega) (by omega)),
            List.getElem_append_left before]
        exact contents i before
      · have endIndex : i = xs.length := by simp at hi; omega
        subst i
        simp [lengthEq]

theorem popRefines (zero : α) {q : Queue α c} (r : Represents q xs) :
    Represents (pop zero q).1 (fifoPop zero xs).1 ∧
    (pop zero q).2 = (fifoPop zero xs).2 := by
  cases xs with
  | nil =>
    have h : q.size = 0 := r.lengthEq
    simpa [pop, h, fifoPop] using r
  | cons x xs =>
    obtain ⟨headBound, sizeBound, lengthEq, contents⟩ := r
    simp only [List.length_cons] at lengthEq
    have headLt : q.head < c := by omega
    have nonempty : q.size ≠ 0 := by omega
    have first := contents 0 (by simp)
    simp [slot, Nat.mod_eq_of_lt headLt] at first
    simp only [pop, nonempty, ite_false, fifoPop]
    refine ⟨⟨Or.inl (Nat.mod_lt _ (by omega)), by dsimp; omega, by dsimp; omega, ?_⟩, ?_⟩
    · intro i hi
      have advance : slot ((q.head + 1) % c) i c = slot q.head (i + 1) c := by
        simp [slot, Nat.add_assoc, Nat.add_comm 1 i]
      have distinct := slotNe (head := q.head) (i := i+1) (j := 0) headLt (by omega) (by omega) (by omega)
      have headSlot : slot q.head 0 c = q.head := by simp [slot, Nat.mod_eq_of_lt headLt]
      rw [headSlot] at distinct
      dsimp
      rw [advance, ite_eq_right distinct]
      exact contents (i+1) (by simp; omega)
    · simp [first]

inductive Action (α : Type) where
  | push (value : α)
  | pop

inductive Reply (α : Type) where
  | pushed (accepted : Bool)
  | popped (value : α) (ok : Bool)

def run (pushStep : σ → α → σ × Bool) (popStep : σ → σ × (α × Bool)) :
    σ → List (Action α) → σ × List (Reply α)
  | s, [] => (s, [])
  | s, .push v :: more =>
    let (next, accepted) := pushStep s v
    let (last, replies) := run pushStep popStep next more
    (last, .pushed accepted :: replies)
  | s, .pop :: more =>
    let (next, v, ok) := popStep s
    let (last, replies) := run pushStep popStep next more
    (last, .popped v ok :: replies)

theorem traceRefines (zero : α) {q : Queue α c} {xs : List α}
    (initial : Represents q xs) (actions : List (Action α)) :
    let actual := run push (pop zero) q actions
    let spec := run (fifoPush c) (fifoPop zero) xs actions
    Represents actual.1 spec.1 ∧ actual.2 = spec.2 := by
  induction actions generalizing q xs with
  | nil => exact ⟨initial, rfl⟩
  | cons a more ih =>
    cases a with
    | push v =>
      obtain ⟨next, same⟩ := pushRefines initial v
      obtain ⟨last, replies⟩ := ih next
      exact ⟨last, by dsimp [run]; rw [same, replies]⟩
    | pop =>
      obtain ⟨next, same⟩ := popRefines zero initial
      obtain ⟨last, replies⟩ := ih next
      exact ⟨last, by dsimp [run]; rw [congrArg Prod.fst same, congrArg Prod.snd same, replies]⟩

end BoundedQueue
