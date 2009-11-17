// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// A parser for Go source files. Input may be provided in a variety of
// forms (see the various Parse* functions); the output is an abstract
// syntax tree (AST) representing the Go source. The parser is invoked
// through one of the Parse* functions.
//
package parser

import (
	"container/vector";
	"fmt";
	"go/ast";
	"go/scanner";
	"go/token";
)


// noPos is used when there is no corresponding source position for a token.
var noPos token.Position


// The mode parameter to the Parse* functions is a set of flags (or 0).
// They control the amount of source code parsed and other optional
// parser functionality.
//
const (
	PackageClauseOnly	uint	= 1 << iota;	// parsing stops after package clause
	ImportsOnly;			// parsing stops after import declarations
	ParseComments;			// parse comments and add them to AST
	Trace;				// print a trace of parsed productions
)


// The parser structure holds the parser's internal state.
type parser struct {
	scanner.ErrorVector;
	scanner	scanner.Scanner;

	// Tracing/debugging
	mode	uint;	// parsing mode
	trace	bool;	// == (mode & Trace != 0)
	indent	uint;	// indentation used for tracing output

	// Comments
	comments	*ast.CommentGroup;	// list of collected comments
	lastComment	*ast.CommentGroup;	// last comment in the comments list
	leadComment	*ast.CommentGroup;	// the last lead comment
	lineComment	*ast.CommentGroup;	// the last line comment

	// Next token
	pos	token.Position;	// token position
	tok	token.Token;	// one token look-ahead
	lit	[]byte;		// token literal

	// Non-syntactic parser control
	optSemi	bool;	// true if semicolon separator is optional in statement list
	exprLev	int;	// < 0: in control clause, >= 0: in expression

	// Scopes
	pkgScope	*ast.Scope;
	fileScope	*ast.Scope;
	topScope	*ast.Scope;
}


// scannerMode returns the scanner mode bits given the parser's mode bits.
func scannerMode(mode uint) uint {
	if mode&ParseComments != 0 {
		return scanner.ScanComments
	}
	return 0;
}


func (p *parser) init(filename string, src []byte, mode uint) {
	p.ErrorVector.Init();
	p.scanner.Init(filename, src, p, scannerMode(mode));
	p.mode = mode;
	p.trace = mode&Trace != 0;	// for convenience (p.trace is used frequently)
	p.next();
}


// ----------------------------------------------------------------------------
// Parsing support

func (p *parser) printTrace(a ...) {
	const dots = ". . . . . . . . . . . . . . . . . . . . . . . . . . . . . . . . "
		". . . . . . . . . . . . . . . . . . . . . . . . . . . . . . . . ";
	const n = uint(len(dots));
	fmt.Printf("%5d:%3d: ", p.pos.Line, p.pos.Column);
	i := 2 * p.indent;
	for ; i > n; i -= n {
		fmt.Print(dots)
	}
	fmt.Print(dots[0:i]);
	fmt.Println(a);
}


func trace(p *parser, msg string) *parser {
	p.printTrace(msg, "(");
	p.indent++;
	return p;
}


// Usage pattern: defer un(trace(p, "..."));
func un(p *parser) {
	p.indent--;
	p.printTrace(")");
}


// Advance to the next token.
func (p *parser) next0() {
	// Because of one-token look-ahead, print the previous token
	// when tracing as it provides a more readable output. The
	// very first token (p.pos.Line == 0) is not initialized (it
	// is token.ILLEGAL), so don't print it .
	if p.trace && p.pos.Line > 0 {
		s := p.tok.String();
		switch {
		case p.tok.IsLiteral():
			p.printTrace(s, string(p.lit))
		case p.tok.IsOperator(), p.tok.IsKeyword():
			p.printTrace("\"" + s + "\"")
		default:
			p.printTrace(s)
		}
	}

	p.pos, p.tok, p.lit = p.scanner.Scan();
	p.optSemi = false;
}


// Consume a comment and return it and the line on which it ends.
func (p *parser) consumeComment() (comment *ast.Comment, endline int) {
	// /*-style comments may end on a different line than where they start.
	// Scan the comment for '\n' chars and adjust endline accordingly.
	endline = p.pos.Line;
	if p.lit[1] == '*' {
		for _, b := range p.lit {
			if b == '\n' {
				endline++
			}
		}
	}

	comment = &ast.Comment{p.pos, p.lit};
	p.next0();

	return;
}


// Consume a group of adjacent comments, add it to the parser's
// comments list, and return the line of which the last comment
// in the group ends. An empty line or non-comment token terminates
// a comment group.
//
func (p *parser) consumeCommentGroup() int {
	list := vector.New(0);
	endline := p.pos.Line;
	for p.tok == token.COMMENT && endline+1 >= p.pos.Line {
		var comment *ast.Comment;
		comment, endline = p.consumeComment();
		list.Push(comment);
	}

	// convert list
	group := make([]*ast.Comment, list.Len());
	for i := 0; i < list.Len(); i++ {
		group[i] = list.At(i).(*ast.Comment)
	}

	// add comment group to the comments list
	g := &ast.CommentGroup{group, nil};
	if p.lastComment != nil {
		p.lastComment.Next = g
	} else {
		p.comments = g
	}
	p.lastComment = g;

	return endline;
}


// Advance to the next non-comment token. In the process, collect
// any comment groups encountered, and remember the last lead and
// and line comments.
//
// A lead comment is a comment group that starts and ends in a
// line without any other tokens and that is followed by a non-comment
// token on the line immediately after the comment group.
//
// A line comment is a comment group that follows a non-comment
// token on the same line, and that has no tokens after it on the line
// where it ends.
//
// Lead and line comments may be considered documentation that is
// stored in the AST.
//
func (p *parser) next() {
	p.leadComment = nil;
	p.lineComment = nil;
	line := p.pos.Line;	// current line
	p.next0();

	if p.tok == token.COMMENT {
		if p.pos.Line == line {
			// The comment is on same line as previous token; it
			// cannot be a lead comment but may be a line comment.
			endline := p.consumeCommentGroup();
			if p.pos.Line != endline {
				// The next token is on a different line, thus
				// the last comment group is a line comment.
				p.lineComment = p.lastComment
			}
		}

		// consume successor comments, if any
		endline := -1;
		for p.tok == token.COMMENT {
			endline = p.consumeCommentGroup()
		}

		if endline >= 0 && endline+1 == p.pos.Line {
			// The next token is following on the line immediately after the
			// comment group, thus the last comment group is a lead comment.
			p.leadComment = p.lastComment
		}
	}
}


func (p *parser) errorExpected(pos token.Position, msg string) {
	msg = "expected " + msg;
	if pos.Offset == p.pos.Offset {
		// the error happened at the current position;
		// make the error message more specific
		msg += ", found '" + p.tok.String() + "'";
		if p.tok.IsLiteral() {
			msg += " " + string(p.lit)
		}
	}
	p.Error(pos, msg);
}


func (p *parser) expect(tok token.Token) token.Position {
	pos := p.pos;
	if p.tok != tok {
		p.errorExpected(pos, "'"+tok.String()+"'")
	}
	p.next();	// make progress in any case
	return pos;
}


// ----------------------------------------------------------------------------
// Scope support

func openScope(p *parser) *parser {
	p.topScope = ast.NewScope(p.topScope);
	return p;
}


// Usage pattern: defer close(openScope(p));
func close(p *parser)	{ p.topScope = p.topScope.Outer }


func (p *parser) declare(ident *ast.Ident) {
	if !p.topScope.Declare(ident) {
		p.Error(p.pos, "'"+ident.Value+"' declared already")
	}
}


func (p *parser) declareList(idents []*ast.Ident) {
	for _, ident := range idents {
		p.declare(ident)
	}
}


// ----------------------------------------------------------------------------
// Common productions

func (p *parser) parseIdent() *ast.Ident {
	if p.tok == token.IDENT {
		x := &ast.Ident{p.pos, string(p.lit)};
		p.next();
		return x;
	}
	p.expect(token.IDENT);	// use expect() error handling
	return &ast.Ident{p.pos, ""};
}


func (p *parser) parseIdentList() []*ast.Ident {
	if p.trace {
		defer un(trace(p, "IdentList"))
	}

	list := vector.New(0);
	list.Push(p.parseIdent());
	for p.tok == token.COMMA {
		p.next();
		list.Push(p.parseIdent());
	}

	// convert vector
	idents := make([]*ast.Ident, list.Len());
	for i := 0; i < list.Len(); i++ {
		idents[i] = list.At(i).(*ast.Ident)
	}

	return idents;
}


func (p *parser) parseExprList() []ast.Expr {
	if p.trace {
		defer un(trace(p, "ExpressionList"))
	}

	list := vector.New(0);
	list.Push(p.parseExpr());
	for p.tok == token.COMMA {
		p.next();
		list.Push(p.parseExpr());
	}

	// convert list
	exprs := make([]ast.Expr, list.Len());
	for i := 0; i < list.Len(); i++ {
		exprs[i] = list.At(i).(ast.Expr)
	}

	return exprs;
}


// ----------------------------------------------------------------------------
// Types

func (p *parser) parseType() ast.Expr {
	if p.trace {
		defer un(trace(p, "Type"))
	}

	typ := p.tryType();

	if typ == nil {
		p.errorExpected(p.pos, "type");
		p.next();	// make progress
		return &ast.BadExpr{p.pos};
	}

	return typ;
}


func (p *parser) parseQualifiedIdent() ast.Expr {
	if p.trace {
		defer un(trace(p, "QualifiedIdent"))
	}

	var x ast.Expr = p.parseIdent();
	if p.tok == token.PERIOD {
		// first identifier is a package identifier
		p.next();
		sel := p.parseIdent();
		x = &ast.SelectorExpr{x, sel};
	}
	return x;
}


func (p *parser) parseTypeName() ast.Expr {
	if p.trace {
		defer un(trace(p, "TypeName"))
	}

	return p.parseQualifiedIdent();
}


func (p *parser) parseArrayType(ellipsisOk bool) ast.Expr {
	if p.trace {
		defer un(trace(p, "ArrayType"))
	}

	lbrack := p.expect(token.LBRACK);
	var len ast.Expr;
	if ellipsisOk && p.tok == token.ELLIPSIS {
		len = &ast.Ellipsis{p.pos};
		p.next();
	} else if p.tok != token.RBRACK {
		len = p.parseExpr()
	}
	p.expect(token.RBRACK);
	elt := p.parseType();

	return &ast.ArrayType{lbrack, len, elt};
}


func (p *parser) makeIdentList(list *vector.Vector) []*ast.Ident {
	idents := make([]*ast.Ident, list.Len());
	for i := 0; i < list.Len(); i++ {
		ident, isIdent := list.At(i).(*ast.Ident);
		if !isIdent {
			pos := list.At(i).(ast.Expr).Pos();
			p.errorExpected(pos, "identifier");
			idents[i] = &ast.Ident{pos, ""};
		}
		idents[i] = ident;
	}
	return idents;
}


func (p *parser) parseFieldDecl() *ast.Field {
	if p.trace {
		defer un(trace(p, "FieldDecl"))
	}

	doc := p.leadComment;

	// a list of identifiers looks like a list of type names
	list := vector.New(0);
	for {
		// TODO(gri): do not allow ()'s here
		list.Push(p.parseType());
		if p.tok == token.COMMA {
			p.next()
		} else {
			break
		}
	}

	// if we had a list of identifiers, it must be followed by a type
	typ := p.tryType();

	// optional tag
	var tag []*ast.BasicLit;
	if p.tok == token.STRING {
		tag = p.parseStringList(nil)
	}

	// analyze case
	var idents []*ast.Ident;
	if typ != nil {
		// IdentifierList Type
		idents = p.makeIdentList(list)
	} else {
		// Type (anonymous field)
		if list.Len() == 1 {
			// TODO(gri): check that this looks like a type
			typ = list.At(0).(ast.Expr)
		} else {
			p.errorExpected(p.pos, "anonymous field");
			typ = &ast.BadExpr{p.pos};
		}
	}

	return &ast.Field{doc, idents, typ, tag, nil};
}


func (p *parser) parseStructType() *ast.StructType {
	if p.trace {
		defer un(trace(p, "StructType"))
	}

	pos := p.expect(token.STRUCT);
	lbrace := p.expect(token.LBRACE);
	list := vector.New(0);
	for p.tok == token.IDENT || p.tok == token.MUL {
		f := p.parseFieldDecl();
		if p.tok != token.RBRACE {
			p.expect(token.SEMICOLON)
		}
		f.Comment = p.lineComment;
		list.Push(f);
	}
	rbrace := p.expect(token.RBRACE);
	p.optSemi = true;

	// convert vector
	fields := make([]*ast.Field, list.Len());
	for i := list.Len() - 1; i >= 0; i-- {
		fields[i] = list.At(i).(*ast.Field)
	}

	return &ast.StructType{pos, lbrace, fields, rbrace, false};
}


func (p *parser) parsePointerType() *ast.StarExpr {
	if p.trace {
		defer un(trace(p, "PointerType"))
	}

	star := p.expect(token.MUL);
	base := p.parseType();

	return &ast.StarExpr{star, base};
}


func (p *parser) tryParameterType(ellipsisOk bool) ast.Expr {
	if ellipsisOk && p.tok == token.ELLIPSIS {
		pos := p.pos;
		p.next();
		if p.tok != token.RPAREN {
			// "..." always must be at the very end of a parameter list
			p.Error(pos, "expected type, found '...'")
		}
		return &ast.Ellipsis{pos};
	}
	return p.tryType();
}


func (p *parser) parseParameterType(ellipsisOk bool) ast.Expr {
	typ := p.tryParameterType(ellipsisOk);
	if typ == nil {
		p.errorExpected(p.pos, "type");
		p.next();	// make progress
		typ = &ast.BadExpr{p.pos};
	}
	return typ;
}


func (p *parser) parseParameterDecl(ellipsisOk bool) (*vector.Vector, ast.Expr) {
	if p.trace {
		defer un(trace(p, "ParameterDecl"))
	}

	// a list of identifiers looks like a list of type names
	list := vector.New(0);
	for {
		// TODO(gri): do not allow ()'s here
		list.Push(p.parseParameterType(ellipsisOk));
		if p.tok == token.COMMA {
			p.next()
		} else {
			break
		}
	}

	// if we had a list of identifiers, it must be followed by a type
	typ := p.tryParameterType(ellipsisOk);

	return list, typ;
}


func (p *parser) parseParameterList(ellipsisOk bool) []*ast.Field {
	if p.trace {
		defer un(trace(p, "ParameterList"))
	}

	list, typ := p.parseParameterDecl(ellipsisOk);
	if typ != nil {
		// IdentifierList Type
		idents := p.makeIdentList(list);
		list.Init(0);
		list.Push(&ast.Field{nil, idents, typ, nil, nil});

		for p.tok == token.COMMA {
			p.next();
			idents := p.parseIdentList();
			typ := p.parseParameterType(ellipsisOk);
			list.Push(&ast.Field{nil, idents, typ, nil, nil});
		}

	} else {
		// Type { "," Type } (anonymous parameters)
		// convert list of types into list of *Param
		for i := 0; i < list.Len(); i++ {
			list.Set(i, &ast.Field{Type: list.At(i).(ast.Expr)})
		}
	}

	// convert list
	params := make([]*ast.Field, list.Len());
	for i := 0; i < list.Len(); i++ {
		params[i] = list.At(i).(*ast.Field)
	}

	return params;
}


func (p *parser) parseParameters(ellipsisOk bool) []*ast.Field {
	if p.trace {
		defer un(trace(p, "Parameters"))
	}

	var params []*ast.Field;
	p.expect(token.LPAREN);
	if p.tok != token.RPAREN {
		params = p.parseParameterList(ellipsisOk)
	}
	p.expect(token.RPAREN);

	return params;
}


func (p *parser) parseResult() []*ast.Field {
	if p.trace {
		defer un(trace(p, "Result"))
	}

	var results []*ast.Field;
	if p.tok == token.LPAREN {
		results = p.parseParameters(false)
	} else if p.tok != token.FUNC {
		typ := p.tryType();
		if typ != nil {
			results = make([]*ast.Field, 1);
			results[0] = &ast.Field{Type: typ};
		}
	}

	return results;
}


func (p *parser) parseSignature() (params []*ast.Field, results []*ast.Field) {
	if p.trace {
		defer un(trace(p, "Signature"))
	}

	params = p.parseParameters(true);
	results = p.parseResult();

	return;
}


func (p *parser) parseFuncType() *ast.FuncType {
	if p.trace {
		defer un(trace(p, "FuncType"))
	}

	pos := p.expect(token.FUNC);
	params, results := p.parseSignature();

	return &ast.FuncType{pos, params, results};
}


func (p *parser) parseMethodSpec() *ast.Field {
	if p.trace {
		defer un(trace(p, "MethodSpec"))
	}

	doc := p.leadComment;
	var idents []*ast.Ident;
	var typ ast.Expr;
	x := p.parseQualifiedIdent();
	if ident, isIdent := x.(*ast.Ident); isIdent && p.tok == token.LPAREN {
		// method
		idents = []*ast.Ident{ident};
		params, results := p.parseSignature();
		typ = &ast.FuncType{noPos, params, results};
	} else {
		// embedded interface
		typ = x
	}

	return &ast.Field{doc, idents, typ, nil, nil};
}


func (p *parser) parseInterfaceType() *ast.InterfaceType {
	if p.trace {
		defer un(trace(p, "InterfaceType"))
	}

	pos := p.expect(token.INTERFACE);
	lbrace := p.expect(token.LBRACE);
	list := vector.New(0);
	for p.tok == token.IDENT {
		m := p.parseMethodSpec();
		if p.tok != token.RBRACE {
			p.expect(token.SEMICOLON)
		}
		m.Comment = p.lineComment;
		list.Push(m);
	}
	rbrace := p.expect(token.RBRACE);
	p.optSemi = true;

	// convert vector
	methods := make([]*ast.Field, list.Len());
	for i := list.Len() - 1; i >= 0; i-- {
		methods[i] = list.At(i).(*ast.Field)
	}

	return &ast.InterfaceType{pos, lbrace, methods, rbrace, false};
}


func (p *parser) parseMapType() *ast.MapType {
	if p.trace {
		defer un(trace(p, "MapType"))
	}

	pos := p.expect(token.MAP);
	p.expect(token.LBRACK);
	key := p.parseType();
	p.expect(token.RBRACK);
	value := p.parseType();

	return &ast.MapType{pos, key, value};
}


func (p *parser) parseChanType() *ast.ChanType {
	if p.trace {
		defer un(trace(p, "ChanType"))
	}

	pos := p.pos;
	dir := ast.SEND | ast.RECV;
	if p.tok == token.CHAN {
		p.next();
		if p.tok == token.ARROW {
			p.next();
			dir = ast.SEND;
		}
	} else {
		p.expect(token.ARROW);
		p.expect(token.CHAN);
		dir = ast.RECV;
	}
	value := p.parseType();

	return &ast.ChanType{pos, dir, value};
}


func (p *parser) tryRawType(ellipsisOk bool) ast.Expr {
	switch p.tok {
	case token.IDENT:
		return p.parseTypeName()
	case token.LBRACK:
		return p.parseArrayType(ellipsisOk)
	case token.STRUCT:
		return p.parseStructType()
	case token.MUL:
		return p.parsePointerType()
	case token.FUNC:
		return p.parseFuncType()
	case token.INTERFACE:
		return p.parseInterfaceType()
	case token.MAP:
		return p.parseMapType()
	case token.CHAN, token.ARROW:
		return p.parseChanType()
	case token.LPAREN:
		lparen := p.pos;
		p.next();
		typ := p.parseType();
		rparen := p.expect(token.RPAREN);
		return &ast.ParenExpr{lparen, typ, rparen};
	}

	// no type found
	return nil;
}


func (p *parser) tryType() ast.Expr	{ return p.tryRawType(false) }


// ----------------------------------------------------------------------------
// Blocks

func makeStmtList(list *vector.Vector) []ast.Stmt {
	stats := make([]ast.Stmt, list.Len());
	for i := 0; i < list.Len(); i++ {
		stats[i] = list.At(i).(ast.Stmt)
	}
	return stats;
}


func (p *parser) parseStmtList() []ast.Stmt {
	if p.trace {
		defer un(trace(p, "StatementList"))
	}

	list := vector.New(0);
	expectSemi := false;
	for p.tok != token.CASE && p.tok != token.DEFAULT && p.tok != token.RBRACE && p.tok != token.EOF {
		if expectSemi {
			p.expect(token.SEMICOLON);
			expectSemi = false;
		}
		list.Push(p.parseStmt());
		if p.tok == token.SEMICOLON {
			p.next()
		} else if p.optSemi {
			p.optSemi = false	// "consume" optional semicolon
		} else {
			expectSemi = true
		}
	}

	return makeStmtList(list);
}


func (p *parser) parseBlockStmt(idents []*ast.Ident) *ast.BlockStmt {
	if p.trace {
		defer un(trace(p, "BlockStmt"))
	}

	defer close(openScope(p));

	lbrace := p.expect(token.LBRACE);
	list := p.parseStmtList();
	rbrace := p.expect(token.RBRACE);
	p.optSemi = true;

	return &ast.BlockStmt{lbrace, list, rbrace};
}


// ----------------------------------------------------------------------------
// Expressions

func (p *parser) parseStringList(x *ast.BasicLit) []*ast.BasicLit {
	if p.trace {
		defer un(trace(p, "StringList"))
	}

	list := vector.New(0);
	if x != nil {
		list.Push(x)
	}

	for p.tok == token.STRING {
		list.Push(&ast.BasicLit{p.pos, token.STRING, p.lit});
		p.next();
	}

	// convert list
	strings := make([]*ast.BasicLit, list.Len());
	for i := 0; i < list.Len(); i++ {
		strings[i] = list.At(i).(*ast.BasicLit)
	}

	return strings;
}


func (p *parser) parseFuncTypeOrLit() ast.Expr {
	if p.trace {
		defer un(trace(p, "FuncTypeOrLit"))
	}

	typ := p.parseFuncType();
	if p.tok != token.LBRACE {
		// function type only
		return typ
	}

	p.exprLev++;
	body := p.parseBlockStmt(nil);
	p.optSemi = false;	// function body requires separating ";"
	p.exprLev--;

	return &ast.FuncLit{typ, body};
}


// parseOperand may return an expression or a raw type (incl. array
// types of the form [...]T. Callers must verify the result.
//
func (p *parser) parseOperand() ast.Expr {
	if p.trace {
		defer un(trace(p, "Operand"))
	}

	switch p.tok {
	case token.IDENT:
		return p.parseIdent()

	case token.INT, token.FLOAT, token.CHAR, token.STRING:
		x := &ast.BasicLit{p.pos, p.tok, p.lit};
		p.next();
		if p.tok == token.STRING && p.tok == token.STRING {
			return &ast.StringList{p.parseStringList(x)}
		}
		return x;

	case token.LPAREN:
		lparen := p.pos;
		p.next();
		p.exprLev++;
		x := p.parseExpr();
		p.exprLev--;
		rparen := p.expect(token.RPAREN);
		return &ast.ParenExpr{lparen, x, rparen};

	case token.FUNC:
		return p.parseFuncTypeOrLit()

	default:
		t := p.tryRawType(true);	// could be type for composite literal or conversion
		if t != nil {
			return t
		}
	}

	p.errorExpected(p.pos, "operand");
	p.next();	// make progress
	return &ast.BadExpr{p.pos};
}


func (p *parser) parseSelectorOrTypeAssertion(x ast.Expr) ast.Expr {
	if p.trace {
		defer un(trace(p, "SelectorOrTypeAssertion"))
	}

	p.expect(token.PERIOD);
	if p.tok == token.IDENT {
		// selector
		sel := p.parseIdent();
		return &ast.SelectorExpr{x, sel};
	}

	// type assertion
	p.expect(token.LPAREN);
	var typ ast.Expr;
	if p.tok == token.TYPE {
		// type switch: typ == nil
		p.next()
	} else {
		typ = p.parseType()
	}
	p.expect(token.RPAREN);

	return &ast.TypeAssertExpr{x, typ};
}


func (p *parser) parseIndex(x ast.Expr) ast.Expr {
	if p.trace {
		defer un(trace(p, "Index"))
	}

	p.expect(token.LBRACK);
	p.exprLev++;
	begin := p.parseExpr();
	var end ast.Expr;
	if p.tok == token.COLON {
		p.next();
		end = p.parseExpr();
	}
	p.exprLev--;
	p.expect(token.RBRACK);

	return &ast.IndexExpr{x, begin, end};
}


func (p *parser) parseCallOrConversion(fun ast.Expr) *ast.CallExpr {
	if p.trace {
		defer un(trace(p, "CallOrConversion"))
	}

	lparen := p.expect(token.LPAREN);
	var args []ast.Expr;
	if p.tok != token.RPAREN {
		args = p.parseExprList()
	}
	rparen := p.expect(token.RPAREN);

	return &ast.CallExpr{fun, lparen, args, rparen};
}


func (p *parser) parseElement() ast.Expr {
	if p.trace {
		defer un(trace(p, "Element"))
	}

	x := p.parseExpr();
	if p.tok == token.COLON {
		colon := p.pos;
		p.next();
		x = &ast.KeyValueExpr{x, colon, p.parseExpr()};
	}

	return x;
}


func (p *parser) parseElementList() []ast.Expr {
	if p.trace {
		defer un(trace(p, "ElementList"))
	}

	list := vector.New(0);
	for p.tok != token.RBRACE && p.tok != token.EOF {
		list.Push(p.parseElement());
		if p.tok == token.COMMA {
			p.next()
		} else {
			break
		}
	}

	// convert list
	elts := make([]ast.Expr, list.Len());
	for i := 0; i < list.Len(); i++ {
		elts[i] = list.At(i).(ast.Expr)
	}

	return elts;
}


func (p *parser) parseCompositeLit(typ ast.Expr) ast.Expr {
	if p.trace {
		defer un(trace(p, "CompositeLit"))
	}

	lbrace := p.expect(token.LBRACE);
	var elts []ast.Expr;
	if p.tok != token.RBRACE {
		elts = p.parseElementList()
	}
	rbrace := p.expect(token.RBRACE);
	return &ast.CompositeLit{typ, lbrace, elts, rbrace};
}


// TODO(gri): Consider different approach to checking syntax after parsing:
//            Provide a arguments (set of flags) to parsing functions
//            restricting what they are supposed to accept depending
//            on context.

// checkExpr checks that x is an expression (and not a type).
func (p *parser) checkExpr(x ast.Expr) ast.Expr {
	// TODO(gri): should provide predicate in AST nodes
	switch t := x.(type) {
	case *ast.BadExpr:
	case *ast.Ident:
	case *ast.BasicLit:
	case *ast.StringList:
	case *ast.FuncLit:
	case *ast.CompositeLit:
	case *ast.ParenExpr:
	case *ast.SelectorExpr:
	case *ast.IndexExpr:
	case *ast.TypeAssertExpr:
		if t.Type == nil {
			// the form X.(type) is only allowed in type switch expressions
			p.errorExpected(x.Pos(), "expression");
			x = &ast.BadExpr{x.Pos()};
		}
	case *ast.CallExpr:
	case *ast.StarExpr:
	case *ast.UnaryExpr:
		if t.Op == token.RANGE {
			// the range operator is only allowed at the top of a for statement
			p.errorExpected(x.Pos(), "expression");
			x = &ast.BadExpr{x.Pos()};
		}
	case *ast.BinaryExpr:
	default:
		// all other nodes are not proper expressions
		p.errorExpected(x.Pos(), "expression");
		x = &ast.BadExpr{x.Pos()};
	}
	return x;
}


// isTypeName returns true iff x is type name.
func isTypeName(x ast.Expr) bool {
	// TODO(gri): should provide predicate in AST nodes
	switch t := x.(type) {
	case *ast.BadExpr:
	case *ast.Ident:
	case *ast.ParenExpr:
		return isTypeName(t.X)	// TODO(gri): should (TypeName) be illegal?
	case *ast.SelectorExpr:
		return isTypeName(t.X)
	default:
		return false	// all other nodes are not type names
	}
	return true;
}


// isCompositeLitType returns true iff x is a legal composite literal type.
func isCompositeLitType(x ast.Expr) bool {
	// TODO(gri): should provide predicate in AST nodes
	switch t := x.(type) {
	case *ast.BadExpr:
	case *ast.Ident:
	case *ast.ParenExpr:
		return isCompositeLitType(t.X)
	case *ast.SelectorExpr:
		return isTypeName(t.X)
	case *ast.ArrayType:
	case *ast.StructType:
	case *ast.MapType:
	default:
		return false	// all other nodes are not legal composite literal types
	}
	return true;
}


// checkExprOrType checks that x is an expression or a type
// (and not a raw type such as [...]T).
//
func (p *parser) checkExprOrType(x ast.Expr) ast.Expr {
	// TODO(gri): should provide predicate in AST nodes
	switch t := x.(type) {
	case *ast.UnaryExpr:
		if t.Op == token.RANGE {
			// the range operator is only allowed at the top of a for statement
			p.errorExpected(x.Pos(), "expression");
			x = &ast.BadExpr{x.Pos()};
		}
	case *ast.ArrayType:
		if len, isEllipsis := t.Len.(*ast.Ellipsis); isEllipsis {
			p.Error(len.Pos(), "expected array length, found '...'");
			x = &ast.BadExpr{x.Pos()};
		}
	}

	// all other nodes are expressions or types
	return x;
}


func (p *parser) parsePrimaryExpr() ast.Expr {
	if p.trace {
		defer un(trace(p, "PrimaryExpr"))
	}

	x := p.parseOperand();
L:	for {
		switch p.tok {
		case token.PERIOD:
			x = p.parseSelectorOrTypeAssertion(p.checkExpr(x))
		case token.LBRACK:
			x = p.parseIndex(p.checkExpr(x))
		case token.LPAREN:
			x = p.parseCallOrConversion(p.checkExprOrType(x))
		case token.LBRACE:
			if isCompositeLitType(x) && (p.exprLev >= 0 || !isTypeName(x)) {
				x = p.parseCompositeLit(x)
			} else {
				break L
			}
		default:
			break L
		}
	}

	return x;
}


func (p *parser) parseUnaryExpr() ast.Expr {
	if p.trace {
		defer un(trace(p, "UnaryExpr"))
	}

	switch p.tok {
	case token.ADD, token.SUB, token.NOT, token.XOR, token.ARROW, token.AND, token.RANGE:
		pos, op := p.pos, p.tok;
		p.next();
		x := p.parseUnaryExpr();
		return &ast.UnaryExpr{pos, op, p.checkExpr(x)};

	case token.MUL:
		// unary "*" expression or pointer type
		pos := p.pos;
		p.next();
		x := p.parseUnaryExpr();
		return &ast.StarExpr{pos, p.checkExprOrType(x)};
	}

	return p.parsePrimaryExpr();
}


func (p *parser) parseBinaryExpr(prec1 int) ast.Expr {
	if p.trace {
		defer un(trace(p, "BinaryExpr"))
	}

	x := p.parseUnaryExpr();
	for prec := p.tok.Precedence(); prec >= prec1; prec-- {
		for p.tok.Precedence() == prec {
			pos, op := p.pos, p.tok;
			p.next();
			y := p.parseBinaryExpr(prec + 1);
			x = &ast.BinaryExpr{p.checkExpr(x), pos, op, p.checkExpr(y)};
		}
	}

	return x;
}


// TODO(gri): parseExpr may return a type or even a raw type ([..]int) -
//            should reject when a type/raw type is obviously not allowed
func (p *parser) parseExpr() ast.Expr {
	if p.trace {
		defer un(trace(p, "Expression"))
	}

	return p.parseBinaryExpr(token.LowestPrec + 1);
}


// ----------------------------------------------------------------------------
// Statements


func (p *parser) parseSimpleStmt(labelOk bool) ast.Stmt {
	if p.trace {
		defer un(trace(p, "SimpleStmt"))
	}

	x := p.parseExprList();

	switch p.tok {
	case token.COLON:
		// labeled statement
		p.next();
		if labelOk && len(x) == 1 {
			if label, isIdent := x[0].(*ast.Ident); isIdent {
				return &ast.LabeledStmt{label, p.parseStmt()}
			}
		}
		p.Error(x[0].Pos(), "illegal label declaration");
		return &ast.BadStmt{x[0].Pos()};

	case
		token.DEFINE, token.ASSIGN, token.ADD_ASSIGN,
		token.SUB_ASSIGN, token.MUL_ASSIGN, token.QUO_ASSIGN,
		token.REM_ASSIGN, token.AND_ASSIGN, token.OR_ASSIGN,
		token.XOR_ASSIGN, token.SHL_ASSIGN, token.SHR_ASSIGN, token.AND_NOT_ASSIGN:
		// assignment statement
		pos, tok := p.pos, p.tok;
		p.next();
		y := p.parseExprList();
		if len(x) > 1 && len(y) > 1 && len(x) != len(y) {
			p.Error(x[0].Pos(), "arity of lhs doesn't match rhs")
		}
		return &ast.AssignStmt{x, pos, tok, y};
	}

	if len(x) > 1 {
		p.Error(x[0].Pos(), "only one expression allowed")
		// continue with first expression
	}

	if p.tok == token.INC || p.tok == token.DEC {
		// increment or decrement
		s := &ast.IncDecStmt{x[0], p.tok};
		p.next();	// consume "++" or "--"
		return s;
	}

	// expression
	return &ast.ExprStmt{x[0]};
}


func (p *parser) parseCallExpr() *ast.CallExpr {
	x := p.parseExpr();
	if call, isCall := x.(*ast.CallExpr); isCall {
		return call
	}
	p.errorExpected(x.Pos(), "function/method call");
	return nil;
}


func (p *parser) parseGoStmt() ast.Stmt {
	if p.trace {
		defer un(trace(p, "GoStmt"))
	}

	pos := p.expect(token.GO);
	call := p.parseCallExpr();
	if call != nil {
		return &ast.GoStmt{pos, call}
	}
	return &ast.BadStmt{pos};
}


func (p *parser) parseDeferStmt() ast.Stmt {
	if p.trace {
		defer un(trace(p, "DeferStmt"))
	}

	pos := p.expect(token.DEFER);
	call := p.parseCallExpr();
	if call != nil {
		return &ast.DeferStmt{pos, call}
	}
	return &ast.BadStmt{pos};
}


func (p *parser) parseReturnStmt() *ast.ReturnStmt {
	if p.trace {
		defer un(trace(p, "ReturnStmt"))
	}

	pos := p.pos;
	p.expect(token.RETURN);
	var x []ast.Expr;
	if p.tok != token.SEMICOLON && p.tok != token.CASE && p.tok != token.DEFAULT && p.tok != token.RBRACE {
		x = p.parseExprList()
	}

	return &ast.ReturnStmt{pos, x};
}


func (p *parser) parseBranchStmt(tok token.Token) *ast.BranchStmt {
	if p.trace {
		defer un(trace(p, "BranchStmt"))
	}

	s := &ast.BranchStmt{p.pos, tok, nil};
	p.expect(tok);
	if tok != token.FALLTHROUGH && p.tok == token.IDENT {
		s.Label = p.parseIdent()
	}

	return s;
}


func (p *parser) makeExpr(s ast.Stmt) ast.Expr {
	if s == nil {
		return nil
	}
	if es, isExpr := s.(*ast.ExprStmt); isExpr {
		return p.checkExpr(es.X)
	}
	p.Error(s.Pos(), "expected condition, found simple statement");
	return &ast.BadExpr{s.Pos()};
}


func (p *parser) parseControlClause(isForStmt bool) (s1, s2, s3 ast.Stmt) {
	if p.tok != token.LBRACE {
		prevLev := p.exprLev;
		p.exprLev = -1;

		if p.tok != token.SEMICOLON {
			s1 = p.parseSimpleStmt(false)
		}
		if p.tok == token.SEMICOLON {
			p.next();
			if p.tok != token.LBRACE && p.tok != token.SEMICOLON {
				s2 = p.parseSimpleStmt(false)
			}
			if isForStmt {
				// for statements have a 3rd section
				p.expect(token.SEMICOLON);
				if p.tok != token.LBRACE {
					s3 = p.parseSimpleStmt(false)
				}
			}
		} else {
			s1, s2 = nil, s1
		}

		p.exprLev = prevLev;
	}

	return s1, s2, s3;
}


func (p *parser) parseIfStmt() *ast.IfStmt {
	if p.trace {
		defer un(trace(p, "IfStmt"))
	}

	// IfStmt block
	defer close(openScope(p));

	pos := p.expect(token.IF);
	s1, s2, _ := p.parseControlClause(false);
	body := p.parseBlockStmt(nil);
	var else_ ast.Stmt;
	if p.tok == token.ELSE {
		p.next();
		else_ = p.parseStmt();
	}

	return &ast.IfStmt{pos, s1, p.makeExpr(s2), body, else_};
}


func (p *parser) parseCaseClause() *ast.CaseClause {
	if p.trace {
		defer un(trace(p, "CaseClause"))
	}

	// CaseClause block
	defer close(openScope(p));

	// SwitchCase
	pos := p.pos;
	var x []ast.Expr;
	if p.tok == token.CASE {
		p.next();
		x = p.parseExprList();
	} else {
		p.expect(token.DEFAULT)
	}

	colon := p.expect(token.COLON);
	body := p.parseStmtList();

	return &ast.CaseClause{pos, x, colon, body};
}


func (p *parser) parseTypeList() []ast.Expr {
	if p.trace {
		defer un(trace(p, "TypeList"))
	}

	list := vector.New(0);
	list.Push(p.parseType());
	for p.tok == token.COMMA {
		p.next();
		list.Push(p.parseType());
	}

	// convert list
	exprs := make([]ast.Expr, list.Len());
	for i := 0; i < list.Len(); i++ {
		exprs[i] = list.At(i).(ast.Expr)
	}

	return exprs;
}


func (p *parser) parseTypeCaseClause() *ast.TypeCaseClause {
	if p.trace {
		defer un(trace(p, "TypeCaseClause"))
	}

	// TypeCaseClause block
	defer close(openScope(p));

	// TypeSwitchCase
	pos := p.pos;
	var types []ast.Expr;
	if p.tok == token.CASE {
		p.next();
		types = p.parseTypeList();
	} else {
		p.expect(token.DEFAULT)
	}

	colon := p.expect(token.COLON);
	body := p.parseStmtList();

	return &ast.TypeCaseClause{pos, types, colon, body};
}


func isExprSwitch(s ast.Stmt) bool {
	if s == nil {
		return true
	}
	if e, ok := s.(*ast.ExprStmt); ok {
		if a, ok := e.X.(*ast.TypeAssertExpr); ok {
			return a.Type != nil	// regular type assertion
		}
		return true;
	}
	return false;
}


func (p *parser) parseSwitchStmt() ast.Stmt {
	if p.trace {
		defer un(trace(p, "SwitchStmt"))
	}

	// SwitchStmt block
	defer close(openScope(p));

	pos := p.expect(token.SWITCH);
	s1, s2, _ := p.parseControlClause(false);

	if isExprSwitch(s2) {
		lbrace := p.expect(token.LBRACE);
		cases := vector.New(0);
		for p.tok == token.CASE || p.tok == token.DEFAULT {
			cases.Push(p.parseCaseClause())
		}
		rbrace := p.expect(token.RBRACE);
		p.optSemi = true;
		body := &ast.BlockStmt{lbrace, makeStmtList(cases), rbrace};
		return &ast.SwitchStmt{pos, s1, p.makeExpr(s2), body};
	}

	// type switch
	// TODO(gri): do all the checks!
	lbrace := p.expect(token.LBRACE);
	cases := vector.New(0);
	for p.tok == token.CASE || p.tok == token.DEFAULT {
		cases.Push(p.parseTypeCaseClause())
	}
	rbrace := p.expect(token.RBRACE);
	p.optSemi = true;
	body := &ast.BlockStmt{lbrace, makeStmtList(cases), rbrace};
	return &ast.TypeSwitchStmt{pos, s1, s2, body};
}


func (p *parser) parseCommClause() *ast.CommClause {
	if p.trace {
		defer un(trace(p, "CommClause"))
	}

	// CommClause block
	defer close(openScope(p));

	// CommCase
	pos := p.pos;
	var tok token.Token;
	var lhs, rhs ast.Expr;
	if p.tok == token.CASE {
		p.next();
		if p.tok == token.ARROW {
			// RecvExpr without assignment
			rhs = p.parseExpr()
		} else {
			// SendExpr or RecvExpr
			rhs = p.parseExpr();
			if p.tok == token.ASSIGN || p.tok == token.DEFINE {
				// RecvExpr with assignment
				tok = p.tok;
				p.next();
				lhs = rhs;
				if p.tok == token.ARROW {
					rhs = p.parseExpr()
				} else {
					p.expect(token.ARROW)	// use expect() error handling
				}
			}
			// else SendExpr
		}
	} else {
		p.expect(token.DEFAULT)
	}

	colon := p.expect(token.COLON);
	body := p.parseStmtList();

	return &ast.CommClause{pos, tok, lhs, rhs, colon, body};
}


func (p *parser) parseSelectStmt() *ast.SelectStmt {
	if p.trace {
		defer un(trace(p, "SelectStmt"))
	}

	pos := p.expect(token.SELECT);
	lbrace := p.expect(token.LBRACE);
	cases := vector.New(0);
	for p.tok == token.CASE || p.tok == token.DEFAULT {
		cases.Push(p.parseCommClause())
	}
	rbrace := p.expect(token.RBRACE);
	p.optSemi = true;
	body := &ast.BlockStmt{lbrace, makeStmtList(cases), rbrace};

	return &ast.SelectStmt{pos, body};
}


func (p *parser) parseForStmt() ast.Stmt {
	if p.trace {
		defer un(trace(p, "ForStmt"))
	}

	// ForStmt block
	defer close(openScope(p));

	pos := p.expect(token.FOR);
	s1, s2, s3 := p.parseControlClause(true);
	body := p.parseBlockStmt(nil);

	if as, isAssign := s2.(*ast.AssignStmt); isAssign {
		// possibly a for statement with a range clause; check assignment operator
		if as.Tok != token.ASSIGN && as.Tok != token.DEFINE {
			p.errorExpected(as.TokPos, "'=' or ':='");
			return &ast.BadStmt{pos};
		}
		// check lhs
		var key, value ast.Expr;
		switch len(as.Lhs) {
		case 2:
			value = as.Lhs[1];
			fallthrough;
		case 1:
			key = as.Lhs[0]
		default:
			p.errorExpected(as.Lhs[0].Pos(), "1 or 2 expressions");
			return &ast.BadStmt{pos};
		}
		// check rhs
		if len(as.Rhs) != 1 {
			p.errorExpected(as.Rhs[0].Pos(), "1 expressions");
			return &ast.BadStmt{pos};
		}
		if rhs, isUnary := as.Rhs[0].(*ast.UnaryExpr); isUnary && rhs.Op == token.RANGE {
			// rhs is range expression; check lhs
			return &ast.RangeStmt{pos, key, value, as.TokPos, as.Tok, rhs.X, body}
		} else {
			p.errorExpected(s2.Pos(), "range clause");
			return &ast.BadStmt{pos};
		}
	} else {
		// regular for statement
		return &ast.ForStmt{pos, s1, p.makeExpr(s2), s3, body}
	}

	panic();	// unreachable
	return nil;
}


func (p *parser) parseStmt() ast.Stmt {
	if p.trace {
		defer un(trace(p, "Statement"))
	}

	switch p.tok {
	case token.CONST, token.TYPE, token.VAR:
		decl, _ := p.parseDecl(false);	// do not consume trailing semicolon
		return &ast.DeclStmt{decl};
	case
		// tokens that may start a top-level expression
		token.IDENT, token.INT, token.FLOAT, token.CHAR, token.STRING, token.FUNC, token.LPAREN,	// operand
		token.LBRACK, token.STRUCT,	// composite type
		token.MUL, token.AND, token.ARROW, token.ADD, token.SUB, token.XOR:	// unary operators
		return p.parseSimpleStmt(true)
	case token.GO:
		return p.parseGoStmt()
	case token.DEFER:
		return p.parseDeferStmt()
	case token.RETURN:
		return p.parseReturnStmt()
	case token.BREAK, token.CONTINUE, token.GOTO, token.FALLTHROUGH:
		return p.parseBranchStmt(p.tok)
	case token.LBRACE:
		return p.parseBlockStmt(nil)
	case token.IF:
		return p.parseIfStmt()
	case token.SWITCH:
		return p.parseSwitchStmt()
	case token.SELECT:
		return p.parseSelectStmt()
	case token.FOR:
		return p.parseForStmt()
	case token.SEMICOLON, token.RBRACE:
		// don't consume the ";", it is the separator following the empty statement
		return &ast.EmptyStmt{p.pos}
	}

	// no statement found
	p.errorExpected(p.pos, "statement");
	p.next();	// make progress
	return &ast.BadStmt{p.pos};
}


// ----------------------------------------------------------------------------
// Declarations

type parseSpecFunction func(p *parser, doc *ast.CommentGroup, getSemi bool) (spec ast.Spec, gotSemi bool)


// Consume semicolon if there is one and getSemi is set, and get any line comment.
// Return the comment if any and indicate if a semicolon was consumed.
//
func (p *parser) parseComment(getSemi bool) (comment *ast.CommentGroup, gotSemi bool) {
	if getSemi && p.tok == token.SEMICOLON {
		p.next();
		gotSemi = true;
	}
	return p.lineComment, gotSemi;
}


func parseImportSpec(p *parser, doc *ast.CommentGroup, getSemi bool) (spec ast.Spec, gotSemi bool) {
	if p.trace {
		defer un(trace(p, "ImportSpec"))
	}

	var ident *ast.Ident;
	if p.tok == token.PERIOD {
		ident = &ast.Ident{p.pos, "."};
		p.next();
	} else if p.tok == token.IDENT {
		ident = p.parseIdent()
	}

	var path []*ast.BasicLit;
	if p.tok == token.STRING {
		path = p.parseStringList(nil)
	} else {
		p.expect(token.STRING)	// use expect() error handling
	}

	comment, gotSemi := p.parseComment(getSemi);

	return &ast.ImportSpec{doc, ident, path, comment}, gotSemi;
}


func parseConstSpec(p *parser, doc *ast.CommentGroup, getSemi bool) (spec ast.Spec, gotSemi bool) {
	if p.trace {
		defer un(trace(p, "ConstSpec"))
	}

	idents := p.parseIdentList();
	typ := p.tryType();
	var values []ast.Expr;
	if typ != nil || p.tok == token.ASSIGN {
		p.expect(token.ASSIGN);
		values = p.parseExprList();
	}
	comment, gotSemi := p.parseComment(getSemi);

	return &ast.ValueSpec{doc, idents, typ, values, comment}, gotSemi;
}


func parseTypeSpec(p *parser, doc *ast.CommentGroup, getSemi bool) (spec ast.Spec, gotSemi bool) {
	if p.trace {
		defer un(trace(p, "TypeSpec"))
	}

	ident := p.parseIdent();
	typ := p.parseType();
	comment, gotSemi := p.parseComment(getSemi);

	return &ast.TypeSpec{doc, ident, typ, comment}, gotSemi;
}


func parseVarSpec(p *parser, doc *ast.CommentGroup, getSemi bool) (spec ast.Spec, gotSemi bool) {
	if p.trace {
		defer un(trace(p, "VarSpec"))
	}

	idents := p.parseIdentList();
	typ := p.tryType();
	var values []ast.Expr;
	if typ == nil || p.tok == token.ASSIGN {
		p.expect(token.ASSIGN);
		values = p.parseExprList();
	}
	comment, gotSemi := p.parseComment(getSemi);

	return &ast.ValueSpec{doc, idents, typ, values, comment}, gotSemi;
}


func (p *parser) parseGenDecl(keyword token.Token, f parseSpecFunction, getSemi bool) (decl *ast.GenDecl, gotSemi bool) {
	if p.trace {
		defer un(trace(p, keyword.String()+"Decl"))
	}

	doc := p.leadComment;
	pos := p.expect(keyword);
	var lparen, rparen token.Position;
	list := vector.New(0);
	if p.tok == token.LPAREN {
		lparen = p.pos;
		p.next();
		for p.tok != token.RPAREN && p.tok != token.EOF {
			doc := p.leadComment;
			spec, semi := f(p, doc, true);	// consume semicolon if any
			list.Push(spec);
			if !semi {
				break
			}
		}
		rparen = p.expect(token.RPAREN);

		if getSemi && p.tok == token.SEMICOLON {
			p.next();
			gotSemi = true;
		} else {
			p.optSemi = true
		}
	} else {
		spec, semi := f(p, nil, getSemi);
		list.Push(spec);
		gotSemi = semi;
	}

	// convert vector
	specs := make([]ast.Spec, list.Len());
	for i := 0; i < list.Len(); i++ {
		specs[i] = list.At(i).(ast.Spec)
	}

	return &ast.GenDecl{doc, pos, keyword, lparen, specs, rparen}, gotSemi;
}


func (p *parser) parseReceiver() *ast.Field {
	if p.trace {
		defer un(trace(p, "Receiver"))
	}

	pos := p.pos;
	par := p.parseParameters(false);

	// must have exactly one receiver
	if len(par) != 1 || len(par) == 1 && len(par[0].Names) > 1 {
		p.errorExpected(pos, "exactly one receiver");
		return &ast.Field{Type: &ast.BadExpr{noPos}};
	}

	recv := par[0];

	// recv type must be TypeName or *TypeName
	base := recv.Type;
	if ptr, isPtr := base.(*ast.StarExpr); isPtr {
		base = ptr.X
	}
	if !isTypeName(base) {
		p.errorExpected(base.Pos(), "type name")
	}

	return recv;
}


func (p *parser) parseFunctionDecl() *ast.FuncDecl {
	if p.trace {
		defer un(trace(p, "FunctionDecl"))
	}

	doc := p.leadComment;
	pos := p.expect(token.FUNC);

	var recv *ast.Field;
	if p.tok == token.LPAREN {
		recv = p.parseReceiver()
	}

	ident := p.parseIdent();
	params, results := p.parseSignature();

	var body *ast.BlockStmt;
	if p.tok == token.LBRACE {
		body = p.parseBlockStmt(nil)
	}

	return &ast.FuncDecl{doc, recv, ident, &ast.FuncType{pos, params, results}, body};
}


func (p *parser) parseDecl(getSemi bool) (decl ast.Decl, gotSemi bool) {
	if p.trace {
		defer un(trace(p, "Declaration"))
	}

	var f parseSpecFunction;
	switch p.tok {
	case token.CONST:
		f = parseConstSpec

	case token.TYPE:
		f = parseTypeSpec

	case token.VAR:
		f = parseVarSpec

	case token.FUNC:
		decl = p.parseFunctionDecl();
		_, gotSemi := p.parseComment(getSemi);
		return decl, gotSemi;

	default:
		pos := p.pos;
		p.errorExpected(pos, "declaration");
		decl = &ast.BadDecl{pos};
		gotSemi = getSemi && p.tok == token.SEMICOLON;
		p.next();	// make progress in any case
		return decl, gotSemi;
	}

	return p.parseGenDecl(p.tok, f, getSemi);
}


func (p *parser) parseDeclList() []ast.Decl {
	if p.trace {
		defer un(trace(p, "DeclList"))
	}

	list := vector.New(0);
	for p.tok != token.EOF {
		decl, _ := p.parseDecl(true);	// consume optional semicolon
		list.Push(decl);
	}

	// convert vector
	decls := make([]ast.Decl, list.Len());
	for i := 0; i < list.Len(); i++ {
		decls[i] = list.At(i).(ast.Decl)
	}

	return decls;
}


// ----------------------------------------------------------------------------
// Source files

func (p *parser) parseFile() *ast.File {
	if p.trace {
		defer un(trace(p, "File"))
	}

	// file block
	defer close(openScope(p));

	// package clause
	doc := p.leadComment;
	pos := p.expect(token.PACKAGE);
	ident := p.parseIdent();
	var decls []ast.Decl;

	// Don't bother parsing the rest if we had errors already.
	// Likely not a Go source file at all.

	if p.ErrorCount() == 0 && p.mode&PackageClauseOnly == 0 {
		// import decls
		list := vector.New(0);
		for p.tok == token.IMPORT {
			decl, _ := p.parseGenDecl(token.IMPORT, parseImportSpec, true);	// consume optional semicolon
			list.Push(decl);
		}

		if p.mode&ImportsOnly == 0 {
			// rest of package body
			for p.tok != token.EOF {
				decl, _ := p.parseDecl(true);	// consume optional semicolon
				list.Push(decl);
			}
		}

		// convert declaration list
		decls = make([]ast.Decl, list.Len());
		for i := 0; i < list.Len(); i++ {
			decls[i] = list.At(i).(ast.Decl)
		}
	}

	return &ast.File{doc, pos, ident, decls, p.comments};
}
