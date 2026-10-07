package main

// planned lists subcommands that a later ticket will add. Until then, invoking
// one prints "not implemented yet (ticket Mx-yy)" and exits 0, so Makefile
// targets work before the ticket lands. A registered subcommand always takes
// precedence, so later tickets just add their file.
var planned = map[string]string{}
