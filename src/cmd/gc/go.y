// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
 * Go language grammar.
 *
 * The Go semicolon rules are:
 *
 *  1. all statements and declarations are terminated by semicolons
 *  2. semicolons can be omitted at top level.
 *  3. semicolons can be omitted before and after the closing ) or }
 *	on a list of statements or declarations.
 *
 * This is accomplished by calling yyoptsemi() to mark the places
 * where semicolons are optional.  That tells the lexer that if a
 * semicolon isn't the next token, it should insert one for us.
 */

%{
#include "go.h"
%}
%union	{
	Node*		node;
	NodeList*		list;
	Type*		type;
	Sym*		sym;
	struct	Val	val;
	int		lint;
}

// |sed 's/.*	//' |9 fmt -l1 |sort |9 fmt -l50 | sed 's/^/%xxx		/'

%token	<val>	LLITERAL
%token	<lint>	LASOP
%token	<sym>	LBREAK LCASE LCHAN LCOLAS LCONST LCONTINUE LDDD
%token	<sym>	LDEFAULT LDEFER LELSE LFALL LFOR LFUNC LGO LGOTO
%token	<sym>	LIF LIMPORT LINTERFACE LMAKE LMAP LNAME LNEW
%token	<sym>	LPACKAGE LRANGE LRETURN LSELECT LSTRUCT LSWITCH
%token	<sym>	LTYPE LVAR

%token		LANDAND LANDNOT LBODY LCOMM LDEC LEQ LGE LGT
%token		LIGNORE LINC LLE LLSH LLT LNE LOROR LRSH
%token		LSEMIBRACE

%type	<lint>	lbrace import_here
%type	<sym>	sym packname
%type	<val>	oliteral

%type	<node>	stmt ntype
%type	<node>	arg_type
%type	<node>	case caseblock
%type	<node>	compound_stmt dotname embed expr
%type	<node>	expr_or_type
%type	<node>	fndcl fnliteral
%type	<node>	for_body for_header for_stmt if_header if_stmt
%type	<node>	interfacedcl keyval labelname name
%type	<node>	name_or_type non_expr_type
%type	<node>	new_name dcl_name oexpr typedclname
%type	<node>	onew_name
%type	<node>	osimple_stmt pexpr
%type	<node>	pseudocall range_stmt select_stmt
%type	<node>	simple_stmt
%type	<node>	switch_stmt uexpr
%type	<node>	xfndcl typedcl

%type	<list>	xdcl fnbody fnres switch_body loop_body dcl_name_list
%type	<list>	new_name_list expr_list keyval_list braced_keyval_list expr_or_type_list xdcl_list
%type	<list>	oexpr_list oexpr_or_type_list caseblock_list stmt_list oarg_type_list arg_type_list
%type	<list>	interfacedcl_list vardcl vardcl_list structdcl structdcl_list
%type	<list>	common_dcl constdcl constdcl1 constdcl_list typedcl_list

%type	<node>	convtype dotdotdot
%type	<node>	indcl interfacetype structtype ptrtype
%type	<node>	chantype non_chan_type othertype non_fn_type fntype

%type	<sym>	hidden_importsym hidden_pkg_importsym

%type	<node>	hidden_constant hidden_dcl hidden_interfacedcl hidden_structdcl

%type	<list>	hidden_funres
%type	<list>	ohidden_funres
%type	<list>	hidden_funarg_list ohidden_funarg_list
%type	<list>	hidden_interfacedcl_list ohidden_interfacedcl_list
%type	<list>	hidden_structdcl_list ohidden_structdcl_list

%type	<type>	hidden_type hidden_type1 hidden_type2 hidden_pkgtype

%left		LOROR
%left		LANDAND
%left		LCOMM
%left		LEQ LNE LLE LGE LLT LGT
%left		'+' '-' '|' '^'
%left		'*' '/' '%' '&' LLSH LRSH LANDNOT

/*
 * manual override of shift/reduce conflicts.
 * the general form is that we assign a precedence
 * to the token being shifted and then introduce
 * NotToken with lower precedence or PreferToToken with higher
 * and annotate the reducing rule accordingly.
 */
%left		NotPackage
%left		LPACKAGE

%left		NotParen
%left		'('

%left		')'
%left		PreferToRightParen

%left		'.'

%left		'{'

%left		NotSemi
%left		';'

%%
file:
	loadsys
	package
	imports
	xdcl_list
	{
		xtop = concat(xtop, $4);
	}

package:
	%prec NotPackage
	{
		prevlineno = lineno;
		yyerror("package statement must be first");
		mkpackage("main");
	}
|	LPACKAGE sym
	{
		mkpackage($2->name);
	}

/*
 * this loads the definitions for the low-level runtime functions,
 * so that the compiler can generate calls to them,
 * but does not make the name "runtime" visible as a package.
 */
loadsys:
	{
		cannedimports("runtime.builtin", runtimeimport);
	}
	import_package
	import_there
	{
		pkgimportname = S;
	}

imports:
|	imports import

import:
	LIMPORT import_stmt osemi
|	LIMPORT '(' import_stmt_list osemi ')' osemi
|	LIMPORT '(' ')' osemi

import_stmt:
	import_here import_package import_there
	{
		Sym *import, *my;
		Node *pack;

		import = pkgimportname;
		my = pkgmyname;
		pkgmyname = S;
		pkgimportname = S;

		if(import == S)
			break;

		pack = nod(OPACK, N, N);
		pack->sym = import;
		pack->lineno = $1;

		if(my == S)
			my = import;
		if(my->name[0] == '.') {
			importdot(import, pack);
			break;
		}
		if(my->name[0] == '_' && my->name[1] == '\0')
			break;

		// Can end up with my->def->op set to ONONAME
		// if one package refers to p without importing it.
		// Don't want to give an error on a good import
		// in another file.
		if(my->def && my->def->op != ONONAME) {
			lineno = $1;
			redeclare(my, "as imported package name");
		}
		my->def = pack;
		my->lastlineno = $1;
		import->block = 1;	// at top level
	}


import_stmt_list:
	import_stmt
|	import_stmt_list ';' import_stmt

import_here:
	LLITERAL
	{
		// import with original name
		$$ = parserline();
		pkgimportname = S;
		pkgmyname = S;
		importfile(&$1, $$);
	}
|	sym LLITERAL
	{
		// import with given name
		$$ = parserline();
		pkgimportname = S;
		pkgmyname = $1;
		importfile(&$2, $$);
	}
|	'.' LLITERAL
	{
		// import into my name space
		$$ = parserline();
		pkgmyname = lookup(".");
		importfile(&$2, $$);
	}

import_package:
	LPACKAGE sym
	{
		pkgimportname = $2;
		if(strcmp($2->name, "main") == 0)
			yyerror("cannot import package main");

		// TODO(rsc): This should go away once we get
		// rid of the global package name space.
		if(strcmp($2->name, package) == 0 && strcmp(package, "runtime") != 0)
			yyerror("package cannot import itself");
	}

import_there:
	{
		defercheckwidth();
	}
	hidden_import_list '$' '$'
	{
		resumecheckwidth();
		checkimports();
		unimportfile();
	}
|	LIMPORT '$' '$'
	{
		defercheckwidth();
	}
	hidden_import_list '$' '$'
	{
		resumecheckwidth();
		checkimports();
	}

/*
 * declarations
 */
xdcl:
	common_dcl osemi
|	xfndcl osemi
	{
		$$ = list1($1);
	}
|	error osemi
	{
		$$ = nil;
	}
|	';'
	{
		yyerror("empty top-level declaration");
		$$ = nil;
	}

common_dcl:
	LVAR vardcl
	{
		$$ = $2;
		if(yylast == LSEMIBRACE)
			yyoptsemi(0);
	}
|	LVAR '(' vardcl_list osemi ')'
	{
		$$ = $3;
		yyoptsemi(0);
	}
|	LVAR '(' ')'
	{
		$$ = nil;
		yyoptsemi(0);
	}
|	LCONST constdcl
	{
		$$ = $2;
		iota = 0;
		lastconst = nil;
	}
|	LCONST '(' constdcl osemi ')'
	{
		$$ = $3;
		iota = 0;
		lastconst = nil;
		yyoptsemi(0);
	}
|	LCONST '(' constdcl ';' constdcl_list osemi ')'
	{
		$$ = concat($3, $5);
		iota = 0;
		lastconst = nil;
		yyoptsemi(0);
	}
|	LCONST '(' ')'
	{
		$$ = nil;
		yyoptsemi(0);
	}
|	LTYPE typedcl
	{
		$$ = list1($2);
		if(yylast == LSEMIBRACE)
			yyoptsemi(0);
	}
|	LTYPE '(' typedcl_list osemi ')'
	{
		$$ = $3;
		yyoptsemi(0);
	}
|	LTYPE '(' ')'
	{
		$$ = nil;
		yyoptsemi(0);
	}

varoptsemi:
	{
		if(yylast == LSEMIBRACE)
			yyoptsemi('=');
	}

vardcl:
	dcl_name_list ntype varoptsemi
	{
		$$ = variter($1, $2, nil);
	}
|	dcl_name_list ntype varoptsemi '=' expr_list
	{
		$$ = variter($1, $2, $5);
	}
|	dcl_name_list '=' expr_list
	{
		$$ = variter($1, nil, $3);
	}

constdcl:
	dcl_name_list ntype '=' expr_list
	{
		$$ = constiter($1, $2, $4);
	}
|	dcl_name_list '=' expr_list
	{
		$$ = constiter($1, N, $3);
	}

constdcl1:
	constdcl
|	dcl_name_list ntype
	{
		$$ = constiter($1, $2, nil);
	}
|	dcl_name_list
	{
		$$ = constiter($1, N, nil);
	}

typedclname:
	sym
	{
		// different from dclname because the name
		// becomes visible right here, not at the end
		// of the declaration.
		$$ = typedcl0($1);
	}

typedcl:
	typedclname ntype
	{
		$$ = typedcl1($1, $2, 1);
	}

simple_stmt:
	expr
	{
		$$ = $1;
	}
|	expr LASOP expr
	{
		$$ = nod(OASOP, $1, $3);
		$$->etype = $2;			// rathole to pass opcode
	}
|	expr_list '=' expr_list
	{
		if($1->next == nil && $3->next == nil) {
			// simple
			$$ = nod(OAS, $1->n, $3->n);
			break;
		}
		// multiple
		$$ = nod(OAS2, N, N);
		$$->list = $1;
		$$->rlist = $3;
	}
|	expr_list LCOLAS expr_list
	{
		if($3->n->op == OTYPESW) {
			if($3->next != nil)
				yyerror("expr.(type) must be alone in list");
			else if($1->next != nil)
				yyerror("argument count mismatch: %d = %d", count($1), 1);
			$$ = nod(OTYPESW, $1->n, $3->n->right);
			break;
		}
		$$ = colas($1, $3);
	}
|	expr LINC
	{
		$$ = nod(OASOP, $1, nodintconst(1));
		$$->etype = OADD;
	}
|	expr LDEC
	{
		$$ = nod(OASOP, $1, nodintconst(1));
		$$->etype = OSUB;
	}

case:
	LCASE expr_or_type_list ':'
	{
		Node *n;

		// will be converted to OCASE
		// right will point to next case
		// done in casebody()
		poptodcl();
		$$ = nod(OXCASE, N, N);
		$$->list = $2;
		if(typesw != N && typesw->right != N && (n=typesw->right->left) != N) {
			// type switch - declare variable
			n = newname(n->sym);
			n->used = 1;	// TODO(rsc): better job here
			declare(n, dclcontext);
			$$->nname = n;
		}
		break;
	}
|	LCASE name '=' expr ':'
	{
		// will be converted to OCASE
		// right will point to next case
		// done in casebody()
		poptodcl();
		$$ = nod(OXCASE, N, N);
		$$->list = list1(nod(OAS, $2, $4));
	}
|	LCASE name LCOLAS expr ':'
	{
		// will be converted to OCASE
		// right will point to next case
		// done in casebody()
		poptodcl();
		$$ = nod(OXCASE, N, N);
		$$->list = list1(colas(list1($2), list1($4)));
	}
|	LDEFAULT ':'
	{
		Node *n;

		poptodcl();
		$$ = nod(OXCASE, N, N);
		if(typesw != N && typesw->right != N && (n=typesw->right->left) != N) {
			// type switch - declare variable
			n = newname(n->sym);
			n->used = 1;	// TODO(rsc): better job here
			declare(n, dclcontext);
			$$->nname = n;
		}
	}

compound_stmt:
	'{'
	{
		markdcl();
	}
	stmt_list '}'
	{
		$$ = liststmt($3);
		popdcl();
		yyoptsemi(0);
	}

switch_body:
	LBODY
	{
		markdcl();
	}
	caseblock_list '}'
	{
		$$ = $3;
		popdcl();
		yyoptsemi(0);
	}

caseblock:
	case stmt_list
	{
		$$ = $1;
		$$->nbody = $2;
	}

caseblock_list:
	{
		$$ = nil;
	}
|	caseblock_list caseblock
	{
		$$ = list($1, $2);
	}

loop_body:
	LBODY
	{
		markdcl();
	}
	stmt_list '}'
	{
		$$ = $3;
		popdcl();
	}

range_stmt:
	expr_list '=' LRANGE expr
	{
		$$ = nod(ORANGE, N, $4);
		$$->list = $1;
		$$->etype = 0;	// := flag
	}
|	expr_list LCOLAS LRANGE expr
	{
		$$ = nod(ORANGE, N, $4);
		$$->list = $1;
		$$->colas = 1;
		colasdefn($1, $$);
	}

for_header:
	osimple_stmt ';' osimple_stmt ';' osimple_stmt
	{
		// init ; test ; incr
		if($5 != N && $5->colas != 0)
			yyerror("cannot declare in the for-increment");
		$$ = nod(OFOR, N, N);
		if($1 != N)
			$$->ninit = list1($1);
		$$->ntest = $3;
		$$->nincr = $5;
	}
|	osimple_stmt
	{
		// normal test
		$$ = nod(OFOR, N, N);
		$$->ntest = $1;
	}
|	range_stmt

for_body:
	for_header loop_body
	{
		$$ = $1;
		$$->nbody = concat($$->nbody, $2);
		yyoptsemi(0);
	}

for_stmt:
	LFOR
	{
		markdcl();
	}
	for_body
	{
		$$ = $3;
		popdcl();
	}

if_header:
	osimple_stmt
	{
		// test
		$$ = nod(OIF, N, N);
		$$->ntest = $1;
	}
|	osimple_stmt ';' osimple_stmt
	{
		// init ; test
		$$ = nod(OIF, N, N);
		if($1 != N)
			$$->ninit = list1($1);
		$$->ntest = $3;
	}

if_stmt:
	LIF
	{
		markdcl();
	}
	if_header loop_body
	{
		$$ = $3;
		$$->nbody = $4;
		// no popdcl; maybe there's an LELSE
		yyoptsemi(LELSE);
	}

switch_stmt:
	LSWITCH
	{
		markdcl();
	}
	if_header
	{
		Node *n;
		n = $3->ntest;
		if(n != N && n->op != OTYPESW)
			n = N;
		typesw = nod(OXXX, typesw, n);
	}
	switch_body
	{
		$$ = $3;
		$$->op = OSWITCH;
		$$->list = $5;
		typesw = typesw->left;
		popdcl();
	}

select_stmt:
	LSELECT
	{
		markdcl();
	}
	switch_body
	{
		$$ = nod(OSELECT, N, N);
		$$->list = $3;
		popdcl();
	}

/*
 * expressions
 */
expr:
	uexpr
|	expr LOROR expr
	{
		$$ = nod(OOROR, $1, $3);
	}
|	expr LANDAND expr
	{
		$$ = nod(OANDAND, $1, $3);
	}
|	expr LEQ expr
	{
		$$ = nod(OEQ, $1, $3);
	}
|	expr LNE expr
	{
		$$ = nod(ONE, $1, $3);
	}
|	expr LLT expr
	{
		$$ = nod(OLT, $1, $3);
	}
|	expr LLE expr
	{
		$$ = nod(OLE, $1, $3);
	}
|	expr LGE expr
	{
		$$ = nod(OGE, $1, $3);
	}
|	expr LGT expr
	{
		$$ = nod(OGT, $1, $3);
	}
|	expr '+' expr
	{
		$$ = nod(OADD, $1, $3);
	}
|	expr '-' expr
	{
		$$ = nod(OSUB, $1, $3);
	}
|	expr '|' expr
	{
		$$ = nod(OOR, $1, $3);
	}
|	expr '^' expr
	{
		$$ = nod(OXOR, $1, $3);
	}
|	expr '*' expr
	{
		$$ = nod(OMUL, $1, $3);
	}
|	expr '/' expr
	{
		$$ = nod(ODIV, $1, $3);
	}
|	expr '%' expr
	{
		$$ = nod(OMOD, $1, $3);
	}
|	expr '&' expr
	{
		$$ = nod(OAND, $1, $3);
	}
|	expr LANDNOT expr
	{
		$$ = nod(OANDNOT, $1, $3);
	}
|	expr LLSH expr
	{
		$$ = nod(OLSH, $1, $3);
	}
|	expr LRSH expr
	{
		$$ = nod(ORSH, $1, $3);
	}
|	expr LCOMM expr
	{
		$$ = nod(OSEND, $1, $3);
	}

uexpr:
	pexpr
|	'*' uexpr
	{
		$$ = nod(OIND, $2, N);
	}
|	'&' uexpr
	{
		$$ = nod(OADDR, $2, N);
	}
|	'+' uexpr
	{
		$$ = nod(OPLUS, $2, N);
	}
|	'-' uexpr
	{
		$$ = nod(OMINUS, $2, N);
	}
|	'!' uexpr
	{
		$$ = nod(ONOT, $2, N);
	}
|	'~' uexpr
	{
		yyerror("the OCOM operator is ^");
		$$ = nod(OCOM, $2, N);
	}
|	'^' uexpr
	{
		$$ = nod(OCOM, $2, N);
	}
|	LCOMM uexpr
	{
		$$ = nod(ORECV, $2, N);
	}

/*
 * call-like statements that
 * can be preceded by 'defer' and 'go'
 */
pseudocall:
	pexpr '(' oexpr_or_type_list ')'
	{
		$$ = nod(OCALL, $1, N);
		$$->list = $3;
	}

pexpr:
	LLITERAL
	{
		$$ = nodlit($1);
	}
|	name
|	pexpr '.' sym
	{
		if($1->op == OPACK) {
			Sym *s;
			s = restrictlookup($3->name, $1->sym->name);
			$1->used = 1;
			$$ = oldname(s);
			break;
		}
		$$ = nod(OXDOT, $1, newname($3));
	}
|	'(' expr_or_type ')'
	{
		$$ = $2;
	}
|	pexpr '.' '(' expr_or_type ')'
	{
		$$ = nod(ODOTTYPE, $1, $4);
	}
|	pexpr '.' '(' LTYPE ')'
	{
		$$ = nod(OTYPESW, N, $1);
	}
|	pexpr '[' expr ']'
	{
		$$ = nod(OINDEX, $1, $3);
	}
|	pexpr '[' keyval ']'
	{
		$$ = nod(OSLICE, $1, $3);
	}
|	pseudocall
|	convtype '(' expr ')'
	{
		// conversion
		$$ = nod(OCALL, $1, N);
		$$->list = list1($3);
	}
|	convtype lbrace braced_keyval_list '}'
	{
		// composite expression
		$$ = nod(OCOMPLIT, N, $1);
		$$->list = $3;

		// If the opening brace was an LBODY,
		// set up for another one now that we're done.
		// See comment in lex.c about loophack.
		if($2 == LBODY)
			loophack = 1;
	}
|	pexpr '{' braced_keyval_list '}'
	{
		// composite expression
		$$ = nod(OCOMPLIT, N, $1);
		$$->list = $3;
	}
|	fnliteral

expr_or_type:
	expr
|	non_expr_type	%prec PreferToRightParen

name_or_type:
	ntype

lbrace:
	LBODY
	{
		$$ = LBODY;
	}
|	'{'
	{
		$$ = '{';
	}

/*
 * names and types
 *	newname is used before declared
 *	oldname is used after declared
 */
new_name:
	sym
	{
		$$ = newname($1);
	}

dcl_name:
	sym
	{
		$$ = dclname($1);
	}

onew_name:
	{
		$$ = N;
	}
|	new_name

sym:
	LNAME

name:
	sym
	{
		$$ = oldname($1);
		if($$->pack != N)
			$$->pack->used = 1;
	}

labelname:
	new_name

convtype:
	'[' oexpr ']' ntype
	{
		// array literal
		$$ = nod(OTARRAY, $2, $4);
	}
|	'[' dotdotdot ']' ntype
	{
		// array literal of nelem
		$$ = nod(OTARRAY, $2, $4);
	}
|	LMAP '[' ntype ']' ntype
	{
		// map literal
		$$ = nod(OTMAP, $3, $5);
	}
|	structtype

/*
 * to avoid parsing conflicts, type is split into
 *	channel types
 *	function types
 *	parenthesized types
 *	any other type
 * the type system makes additional restrictions,
 * but those are not implemented in the grammar.
 */
dotdotdot:
	LDDD
	{
		$$ = typenod(typ(TDDD));
	}

ntype:
	chantype
|	fntype
|	othertype
|	ptrtype
|	dotname
|	'(' ntype ')'
	{
		$$ = $2;
	}

non_expr_type:
	chantype
|	fntype
|	othertype
|	'*' non_expr_type
	{
		$$ = nod(OIND, $2, N);
	}
|	'(' non_expr_type ')'
	{
		$$ = $2;
	}

non_chan_type:
	fntype
|	othertype
|	ptrtype
|	dotname
|	'(' ntype ')'
	{
		$$ = $2;
	}

non_fn_type:
	chantype
|	othertype
|	ptrtype
|	dotname

dotname:
	name
|	name '.' sym
	{
		if($1->op == OPACK) {
			Sym *s;
			s = restrictlookup($3->name, $1->sym->name);
			$1->used = 1;
			$$ = oldname(s);
			break;
		}
		$$ = nod(OXDOT, $1, newname($3));
	}

othertype:
	'[' oexpr ']' ntype
	{
		$$ = nod(OTARRAY, $2, $4);
	}
|	'[' dotdotdot ']' ntype
	{
		// array literal of nelem
		$$ = nod(OTARRAY, $2, $4);
	}
|	LCOMM LCHAN ntype
	{
		$$ = nod(OTCHAN, $3, N);
		$$->etype = Crecv;
	}
|	LCHAN LCOMM non_chan_type
	{
		$$ = nod(OTCHAN, $3, N);
		$$->etype = Csend;
	}
|	LMAP '[' ntype ']' ntype
	{
		$$ = nod(OTMAP, $3, $5);
	}
|	structtype
|	interfacetype

ptrtype:
	'*' ntype
	{
		$$ = nod(OIND, $2, N);
	}

chantype:
	LCHAN ntype
	{
		$$ = nod(OTCHAN, $2, N);
		$$->etype = Cboth;
	}

structtype:
	LSTRUCT '{' structdcl_list osemi '}'
	{
		$$ = nod(OTSTRUCT, N, N);
		$$->list = $3;
		// Distinguish closing brace in struct from
		// other closing braces by explicitly marking it.
		// Used above (yylast == LSEMIBRACE).
		yylast = LSEMIBRACE;
	}
|	LSTRUCT '{' '}'
	{
		$$ = nod(OTSTRUCT, N, N);
		yylast = LSEMIBRACE;
	}

interfacetype:
	LINTERFACE '{' interfacedcl_list osemi '}'
	{
		$$ = nod(OTINTER, N, N);
		$$->list = $3;
		yylast = LSEMIBRACE;
	}
|	LINTERFACE '{' '}'
	{
		$$ = nod(OTINTER, N, N);
		yylast = LSEMIBRACE;
	}

keyval:
	expr ':' expr
	{
		$$ = nod(OKEY, $1, $3);
	}


/*
 * function stuff
 * all in one place to show how crappy it all is
 */
xfndcl:
	LFUNC fndcl fnbody
	{
		$$ = $2;
		$$->nbody = $3;
		funcbody($$);
	}

fndcl:
	dcl_name '(' oarg_type_list ')' fnres
	{
		Node *n;

		$$ = nod(ODCLFUNC, N, N);
		$$->nname = $1;
		if($3 == nil && $5 == nil)
			$$->nname = renameinit($1);
		n = nod(OTFUNC, N, N);
		n->list = $3;
		n->rlist = $5;
		// TODO: check if nname already has an ntype
		$$->nname->ntype = n;
		funchdr($$);
	}
|	'(' oarg_type_list ')' new_name '(' oarg_type_list ')' fnres
	{
		Node *rcvr, *t;

		rcvr = $2->n;
		if($2->next != nil || $2->n->op != ODCLFIELD) {
			yyerror("bad receiver in method");
			rcvr = N;
		}

		$$ = nod(ODCLFUNC, N, N);
		$$->nname = methodname1($4, rcvr->right);
		t = nod(OTFUNC, rcvr, N);
		t->list = $6;
		t->rlist = $8;
		$$->nname->ntype = t;
		$$->shortname = $4;
		funchdr($$);
	}

fntype:
	LFUNC '(' oarg_type_list ')' fnres
	{
		$$ = nod(OTFUNC, N, N);
		$$->list = $3;
		$$->rlist = $5;
	}

fnbody:
	{
		$$ = nil;
	}
|	'{' stmt_list '}'
	{
		$$ = $2;
		if($$ == nil)
			$$ = list1(nod(OEMPTY, N, N));
		yyoptsemi(0);
	}

fnres:
	%prec NotParen
	{
		$$ = nil;
	}
|	non_fn_type
	{
		$$ = list1(nod(ODCLFIELD, N, $1));
	}
|	'(' oarg_type_list ')'
	{
		$$ = $2;
	}

fnlitdcl:
	fntype
	{
		closurehdr($1);
	}

fnliteral:
	fnlitdcl '{' stmt_list '}'
	{
		$$ = closurebody($3);
	}


/*
 * lists of things
 * note that they are left recursive
 * to conserve yacc stack. they need to
 * be reversed to interpret correctly
 */
xdcl_list:
	{
		$$ = nil;
	}
|	xdcl_list xdcl
	{
		$$ = concat($1, $2);
		if(nsyntaxerrors == 0)
			testdclstack();
	}

vardcl_list:
	vardcl
|	vardcl_list ';' vardcl
	{
		$$ = concat($1, $3);
	}

constdcl_list:
	constdcl1
|	constdcl_list ';' constdcl1
	{
		$$ = concat($1, $3);
	}

typedcl_list:
	typedcl
	{
		$$ = list1($1);
	}
|	typedcl_list ';' typedcl
	{
		$$ = list($1, $3);
	}

structdcl_list:
	structdcl
|	structdcl_list ';' structdcl
	{
		$$ = concat($1, $3);
	}

interfacedcl_list:
	interfacedcl
	{
		$$ = list1($1);
	}
|	interfacedcl_list ';' interfacedcl
	{
		$$ = list($1, $3);
	}

structdcl:
	new_name_list ntype oliteral
	{
		NodeList *l;

		for(l=$1; l; l=l->next) {
			l->n = nod(ODCLFIELD, l->n, $2);
			l->n->val = $3;
		}
	}
|	embed oliteral
	{
		$1->val = $2;
		$$ = list1($1);
	}
|	'*' embed oliteral
	{
		$2->right = nod(OIND, $2->right, N);
		$2->val = $3;
		$$ = list1($2);
	}

packname:
	LNAME
	{
		Node *n;

		$$ = $1;
		n = oldname($1);
		if(n->pack != N)
			n->pack->used = 1;
	}
|	LNAME '.' sym
	{
		char *pkg;

		if($1->def == N || $1->def->op != OPACK) {
			yyerror("%S is not a package", $1);
			pkg = $1->name;
		} else {
			$1->def->used = 1;
			pkg = $1->def->sym->name;
		}
		$$ = restrictlookup($3->name, pkg);
	}

embed:
	packname
	{
		$$ = embedded($1);
	}

interfacedcl:
	new_name indcl
	{
		$$ = nod(ODCLFIELD, $1, $2);
	}
|	packname
	{
		$$ = nod(ODCLFIELD, N, oldname($1));
	}

indcl:
	'(' oarg_type_list ')' fnres
	{
		// without func keyword
		$$ = nod(OTFUNC, fakethis(), N);
		$$->list = $2;
		$$->rlist = $4;
	}

/*
 * function arguments.
 */
arg_type:
	name_or_type
|	sym name_or_type
	{
		$$ = nod(ONONAME, N, N);
		$$->sym = $1;
		$$ = nod(OKEY, $$, $2);
	}
|	sym dotdotdot
	{
		$$ = nod(ONONAME, N, N);
		$$->sym = $1;
		$$ = nod(OKEY, $$, $2);
	}
|	dotdotdot

arg_type_list:
	arg_type
	{
		$$ = list1($1);
	}
|	arg_type_list ',' arg_type
	{
		$$ = list($1, $3);
	}

oarg_type_list:
	{
		$$ = nil;
	}
|	arg_type_list
	{
		$$ = checkarglist($1);
	}

/*
 * statement
 */
stmt:
	{
		$$ = N;
	}
|	simple_stmt
|	compound_stmt
|	common_dcl
	{
		$$ = liststmt($1);
	}
|	for_stmt
|	switch_stmt
|	select_stmt
|	if_stmt
	{
		popdcl();
		$$ = $1;
	}
|	if_stmt LELSE stmt
	{
		popdcl();
		$$ = $1;
		$$->nelse = list1($3);
	}
|	error
	{
		$$ = N;
	}
|	labelname ':' stmt
	{
		NodeList *l;

		l = list1(nod(OLABEL, $1, $3));
		if($3)
			l = list(l, $3);
		$$ = liststmt(l);
	}
|	LFALL
	{
		// will be converted to OFALL
		$$ = nod(OXFALL, N, N);
	}
|	LBREAK onew_name
	{
		$$ = nod(OBREAK, $2, N);
	}
|	LCONTINUE onew_name
	{
		$$ = nod(OCONTINUE, $2, N);
	}
|	LGO pseudocall
	{
		$$ = nod(OPROC, $2, N);
	}
|	LDEFER pseudocall
	{
		$$ = nod(ODEFER, $2, N);
	}
|	LGOTO new_name
	{
		$$ = nod(OGOTO, $2, N);
	}
|	LRETURN oexpr_list
	{
		$$ = nod(ORETURN, N, N);
		$$->list = $2;
	}

stmt_list:
	stmt
	{
		$$ = nil;
		if($1 != N)
			$$ = list1($1);
	}
|	stmt_list ';' stmt
	{
		$$ = $1;
		if($3 != N)
			$$ = list($$, $3);
	}

new_name_list:
	new_name
	{
		$$ = list1($1);
	}
|	new_name_list ',' new_name
	{
		$$ = list($1, $3);
	}

dcl_name_list:
	dcl_name
	{
		$$ = list1($1);
	}
|	dcl_name_list ',' dcl_name
	{
		$$ = list($1, $3);
	}

expr_list:
	expr
	{
		$$ = list1($1);
	}
|	expr_list ',' expr
	{
		$$ = list($1, $3);
	}

expr_or_type_list:
	expr_or_type
	{
		$$ = list1($1);
	}
|	expr_or_type_list ',' expr_or_type
	{
		$$ = list($1, $3);
	}

/*
 * list of combo of keyval and val
 */
keyval_list:
	keyval
	{
		$$ = list1($1);
	}
|	expr
	{
		$$ = list1($1);
	}
|	keyval_list ',' keyval
	{
		$$ = list($1, $3);
	}
|	keyval_list ',' expr
	{
		$$ = list($1, $3);
	}

braced_keyval_list:
	{
		$$ = nil;
	}
|	keyval_list ocomma
	{
		$$ = $1;
	}

/*
 * optional things
 */
osemi:
	%prec NotSemi
|	';'

ocomma:
|	','

oexpr:
	{
		$$ = N;
	}
|	expr

oexpr_list:
	{
		$$ = nil;
	}
|	expr_list

oexpr_or_type_list:
	{
		$$ = nil;
	}
|	expr_or_type_list

osimple_stmt:
	{
		$$ = N;
	}
|	simple_stmt

ohidden_funarg_list:
	{
		$$ = nil;
	}
|	hidden_funarg_list

ohidden_structdcl_list:
	{
		$$ = nil;
	}
|	hidden_structdcl_list

ohidden_interfacedcl_list:
	{
		$$ = nil;
	}
|	hidden_interfacedcl_list

oliteral:
	{
		$$.ctype = CTxxx;
	}
|	LLITERAL

/*
 * import syntax from header of
 * an output package
 */
hidden_import:
	LPACKAGE sym
	/* variables */
|	LVAR hidden_pkg_importsym hidden_type
	{
		importvar($2, $3, PEXTERN);
	}
|	LCONST hidden_pkg_importsym '=' hidden_constant
	{
		importconst($2, types[TIDEAL], $4);
	}
|	LCONST hidden_pkg_importsym hidden_type '=' hidden_constant
	{
		importconst($2, $3, $5);
	}
|	LTYPE hidden_pkgtype hidden_type
	{
		importtype($2, $3);
	}
|	LFUNC hidden_pkg_importsym '(' ohidden_funarg_list ')' ohidden_funres
	{
		importvar($2, functype(N, $4, $6), PFUNC);
	}
|	LFUNC '(' hidden_funarg_list ')' sym '(' ohidden_funarg_list ')' ohidden_funres
	{
		if($3->next != nil || $3->n->op != ODCLFIELD) {
			yyerror("bad receiver in method");
			YYERROR;
		}
		importmethod($5, functype($3->n, $7, $9));
	}

hidden_pkgtype:
	hidden_pkg_importsym
	{
		$$ = pkgtype($1);
		importsym($1, OTYPE);
	}

hidden_type:
	hidden_type1
|	hidden_type2

hidden_type1:
	hidden_importsym
	{
		$$ = pkgtype($1);
	}
|	LNAME
	{
		// predefined name like uint8
		$1 = pkglookup($1->name, "/builtin/");
		if($1->def == N || $1->def->op != OTYPE) {
			yyerror("%s is not a type", $1->name);
			$$ = T;
		} else
			$$ = $1->def->type;
	}
|	'[' ']' hidden_type
	{
		$$ = aindex(N, $3);
	}
|	'[' LLITERAL ']' hidden_type
	{
		$$ = aindex(nodlit($2), $4);
	}
|	LMAP '[' hidden_type ']' hidden_type
	{
		$$ = maptype($3, $5);
	}
|	LSTRUCT '{' ohidden_structdcl_list '}'
	{
		$$ = dostruct($3, TSTRUCT);
	}
|	LINTERFACE '{' ohidden_interfacedcl_list '}'
	{
		$$ = dostruct($3, TINTER);
		$$ = sortinter($$);
	}
|	'*' hidden_type
	{
		$$ = ptrto($2);
	}
|	LCOMM LCHAN hidden_type
	{
		$$ = typ(TCHAN);
		$$->type = $3;
		$$->chan = Crecv;
	}
|	LCHAN LCOMM hidden_type1
	{
		$$ = typ(TCHAN);
		$$->type = $3;
		$$->chan = Csend;
	}
|	LDDD
	{
		$$ = typ(TDDD);
	}

hidden_type2:
	LCHAN hidden_type
	{
		$$ = typ(TCHAN);
		$$->type = $2;
		$$->chan = Cboth;
	}
|	LFUNC '(' ohidden_funarg_list ')' ohidden_funres
	{
		$$ = functype(nil, $3, $5);
	}

hidden_dcl:
	sym hidden_type
	{
		$$ = nod(ODCLFIELD, newname($1), typenod($2));
	}
|	'?' hidden_type
	{
		$$ = nod(ODCLFIELD, N, typenod($2));
	}

hidden_structdcl:
	sym hidden_type oliteral
	{
		$$ = nod(ODCLFIELD, newname($1), typenod($2));
		$$->val = $3;
	}
|	'?' hidden_type oliteral
	{
		Sym *s;

		s = $2->sym;
		if(s == S && isptr[$2->etype])
			s = $2->type->sym;
		if(s && strcmp(s->package, "/builtin/") == 0)
			s = lookup(s->name);
		$$ = embedded(s);
		$$->right = typenod($2);
		$$->val = $3;
	}

hidden_interfacedcl:
	sym '(' ohidden_funarg_list ')' ohidden_funres
	{
		$$ = nod(ODCLFIELD, newname($1), typenod(functype(fakethis(), $3, $5)));
	}

ohidden_funres:
	{
		$$ = nil;
	}
|	hidden_funres

hidden_funres:
	'(' ohidden_funarg_list ')'
	{
		$$ = $2;
	}
|	hidden_type1
	{
		$$ = list1(nod(ODCLFIELD, N, typenod($1)));
	}

hidden_constant:
	LLITERAL
	{
		$$ = nodlit($1);
	}
|	'-' LLITERAL
	{
		$$ = nodlit($2);
		switch($$->val.ctype){
		case CTINT:
			mpnegfix($$->val.u.xval);
			break;
		case CTFLT:
			mpnegflt($$->val.u.fval);
			break;
		default:
			yyerror("bad negated constant");
		}
	}
|	sym
	{
		$$ = oldname(pkglookup($1->name, "/builtin/"));
		if($$->op != OLITERAL)
			yyerror("bad constant %S", $$->sym);
	}

hidden_importsym:
	sym '.' sym
	{
		$$ = pkglookup($3->name, $1->name);
	}

hidden_pkg_importsym:
	hidden_importsym
	{
		$$ = $1;
		structpkg = $$->package;
	}

hidden_import_list:
|	hidden_import_list hidden_import

hidden_funarg_list:
	hidden_dcl
	{
		$$ = list1($1);
	}
|	hidden_funarg_list ',' hidden_dcl
	{
		$$ = list($1, $3);
	}

hidden_structdcl_list:
	hidden_structdcl
	{
		$$ = list1($1);
	}
|	hidden_structdcl_list ';' hidden_structdcl
	{
		$$ = list($1, $3);
	}

hidden_interfacedcl_list:
	hidden_interfacedcl
	{
		$$ = list1($1);
	}
|	hidden_interfacedcl_list ';' hidden_interfacedcl
	{
		$$ = list($1, $3);
	}
