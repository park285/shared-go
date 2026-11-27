SELECT
    (SELECT journal_mode FROM pragma_journal_mode()),
    (SELECT synchronous FROM pragma_synchronous()),
    (SELECT foreign_keys FROM pragma_foreign_keys()),
    (SELECT journal_size_limit FROM pragma_journal_size_limit()),
    (SELECT timeout FROM pragma_busy_timeout())
