package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/cockroachdb/docs/generate/extract"
	"github.com/spf13/cobra"
)

func main() {
	var (
		inputPath  string
		outputPath string
	)

	read := func() io.Reader {
		var r io.Reader = os.Stdin
		if inputPath != "" {
			f, err := os.Open(inputPath)
			if err != nil {
				log.Fatal(err)
			}
			defer f.Close()
			b, err := ioutil.ReadAll(f)
			if err != nil {
				log.Fatal(err)
			}
			r = bytes.NewReader(b)
		}
		return r
	}

	write := func(b []byte) {
		var w io.Writer = os.Stdout
		if outputPath != "" {
			f, err := os.Create(outputPath)
			if err != nil {
				log.Fatal(err)
			}
			defer f.Close()
			w = f
		}
		if _, err := w.Write(b); err != nil {
			log.Fatal(err)
		}
	}

	var addr string

	cmdBNF := &cobra.Command{
		Use:   "bnf",
		Short: "Write EBNF to stdout from sql.y",
		Run: func(cmd *cobra.Command, args []string) {
			b, err := runBNF(addr)
			if err != nil {
				log.Fatal(err)
			}
			write(b)
		},
	}

	cmdBNF.Flags().StringVar(&addr, "addr", "https://raw.githubusercontent.com/cockroachdb/cockroach/master/sql/parser/sql.y", "Location of sql.y file. Can also specify a local file.")

	var (
		topStmt string
		descend bool
		inline  []string
	)

	cmdParse := &cobra.Command{
		Use:   "reduce",
		Short: "Reduces and simplify an EBNF file to a smaller grammar",
		Long:  "Reads from stdin, writes to stdout.",
		Run: func(cmd *cobra.Command, args []string) {
			b, err := runParse(read(), inline, topStmt, descend, true, nil, nil)
			if err != nil {
				log.Fatal(err)
			}
			write(b)
		},
	}

	cmdParse.Flags().StringVar(&topStmt, "stmt", "stmt_block", "Name of top-level statement.")
	cmdParse.Flags().BoolVar(&descend, "descend", true, "Descend past -stmt.")
	cmdParse.Flags().StringSliceVar(&inline, "inline", nil, "List of statements to inline.")

	cmdRR := &cobra.Command{
		Use:   "rr",
		Short: "Generate railroad diagram from stdin, writes to stdout",
		Run: func(cmd *cobra.Command, args []string) {
			b, err := runRR("", read())
			if err != nil {
				log.Fatal(err)
			}
			write(b)
		},
	}

	cmdBody := &cobra.Command{
		Use:   "body",
		Short: "Extract HTML <body> contents from stdin, writes to stdout",
		Run: func(cmd *cobra.Command, args []string) {
			s, err := extract.InnerTag(read(), "body")
			if err != nil {
				log.Fatal(err)
			}
			write([]byte(s))
		},
	}

	cmdFuncs := &cobra.Command{
		Use:   "funcs",
		Short: "Generates functions.md and operators.md",
		Run: func(cmd *cobra.Command, args []string) {
			generateFuncs()
		},
	}

	var (
		baseDir  string
		filter   string
		printBNF bool
	)

	rootCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate SVG diagrams from SQL grammar",
		Long:  `With no arguments, generates SQL diagrams for all statements.`,
		Run: func(cmd *cobra.Command, args []string) {
			bnf, err := runBNF(addr)
			if err != nil {
				log.Fatal(err)
			}
			br := func() io.Reader {
				return bytes.NewReader(bnf)
			}
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				const name = "stmt_block"
				if filter != "" && filter != name {
					return
				}
				g, err := runParse(br(), nil, name, true, true, nil, nil)
				if err != nil {
					log.Fatal(err)
				}
				if printBNF {
					fmt.Printf("%s:\n\n%s\n", name, g)
					return
				}
				rr, err := runRR("stmt_block", bytes.NewReader(g))
				if err != nil {
					log.Fatal(err)
				}
				body, err := extract.InnerTag(bytes.NewReader(rr), "body")
				body = strings.SplitN(body, "<hr/>", 2)[0]
				body += `<p>generated by <a href="http://www.bottlecaps.de/rr/ui">Railroad Diagram Generator</a></p>`
				body = fmt.Sprintf("<div>%s</div>", body)
				if err != nil {
					log.Fatal(err)
				}
				if err := ioutil.WriteFile(filepath.Join(baseDir, "grammar.html"), []byte(body), 0644); err != nil {
					log.Fatal(err)
				}
			}()

			specs := []stmtSpec{
				// TODO(mjibson): improve SET filtering
				// TODO(mjibson): improve SELECT display
				{
					name:       "add_column",
					stmt:       "alter_table_stmt",
					inline:     []string{"alter_table_cmds", "alter_table_cmd"},
					match:      []*regexp.Regexp{regexp.MustCompile(`'ADD' .* column_def \( ','`)},
					regreplace: map[string]string{` \( ','.*\)\*`: ""},
				},
				{
					name:    "alter_table_stmt",
					inline:  []string{"alter_table_cmds", "alter_table_cmd", "column_def", "opt_drop_behavior", "alter_column_default", "opt_column", "opt_set_data"},
					nosplit: true,
				},
				{
					name:   "begin_transaction",
					stmt:   "transaction_stmt",
					inline: []string{"opt_transaction", "opt_transaction_mode_list", "transaction_iso_level", "transaction_user_priority", "user_priority"},
					match:  []*regexp.Regexp{regexp.MustCompile("'BEGIN'|'START'")},
				},
				{name: "column_def"},
				{name: "col_qual_list", stmt: "col_qual_list", inline: []string{"col_qualification", "col_qualification_elem"}, replace: map[string]string{"| 'REFERENCES' qualified_name opt_name_parens": ""}},
				{
					name:   "commit_transaction",
					stmt:   "transaction_stmt",
					inline: []string{"opt_transaction"},
					match:  []*regexp.Regexp{regexp.MustCompile("'COMMIT'|'END'")},
				},
				{name: "create_database_stmt", inline: []string{"opt_encoding_clause"}, replace: map[string]string{"'SCONST'": "encoding"}, unlink: []string{"name", "encoding"}},
				{
					name:   "create_index_stmt",
					inline: []string{"opt_storing", "storing", "opt_unique", "opt_name", "index_params", "index_elem", "opt_asc_desc", "name_list"},
					replace: map[string]string{
						"'INDEX' ( name": "'INDEX' ( index_name",
						"'EXISTS' name":  "'EXISTS' index_name",
						"qualified_name": "table_name",
						"',' name":       "',' column_name",
						"( name (":       "( column_name (",
					},
					unlink:  []string{"index_name", "table_name", "column_name"},
					nosplit: true,
				},
				{name: "create_table_stmt", inline: []string{"opt_table_elem_list", "table_elem_list", "table_elem"}},
				{name: "delete_stmt", inline: []string{"relation_expr_opt_alias", "where_clause", "returning_clause", "target_list", "target_elem"}},
				{
					name:  "drop_database",
					stmt:  "drop_stmt",
					match: []*regexp.Regexp{regexp.MustCompile("'DROP' 'DATABASE'")},
				},
				{
					name:    "drop_index",
					stmt:    "drop_stmt",
					match:   []*regexp.Regexp{regexp.MustCompile("'DROP' 'INDEX'")},
					inline:  []string{"opt_drop_behavior", "table_name_with_index_list", "table_name_with_index"},
					replace: map[string]string{"qualified_name": "table_name", "'@' name": "'@' index_name"}, unlink: []string{"table_name", "index_name"},
				},
				{name: "drop_stmt", inline: []string{"table_name_list", "any_name", "qualified_name_list", "qualified_name"}},
				{
					name:  "drop_table",
					stmt:  "drop_stmt",
					match: []*regexp.Regexp{regexp.MustCompile("'DROP' 'TABLE'")},
				},
				{name: "explain_stmt", inline: []string{"explainable_stmt", "explain_option_list"}},
				{name: "family_def", inline: []string{"opt_name", "name_list"}},
				{
					name:    "grant_stmt",
					inline:  []string{"privileges", "privilege_list", "privilege", "privilege_target", "grantee_list", "table_pattern_list", "name_list"},
					replace: map[string]string{"table_pattern": "table_name", "'DATABASE' ( name ( ',' name )* )": "'DATABASE' ( database_name ( ',' database_name )* )", "'TO' ( name ( ',' name )* )": "'TO' ( user_name ( ',' user_name )* )"},
					unlink:  []string{"table_name", "database_name", "user_name"},
					nosplit: true,
				},
				{name: "index_def", inline: []string{"opt_storing", "storing", "index_params", "opt_name"}},
				{
					name:   "insert_stmt",
					inline: []string{"insert_target", "insert_rest", "returning_clause"},
					match:  []*regexp.Regexp{regexp.MustCompile("'INSERT'")},
				},
				{name: "iso_level"},
				{name: "release_savepoint", stmt: "release_stmt", inline: []string{"savepoint_name"}},
				{name: "rename_column", stmt: "rename_stmt", match: []*regexp.Regexp{regexp.MustCompile("'ALTER' 'TABLE' .* 'RENAME' opt_column")}},
				{name: "rename_database", stmt: "rename_stmt", match: []*regexp.Regexp{regexp.MustCompile("'ALTER' 'DATABASE'")}},
				{name: "rename_index", stmt: "rename_stmt", match: []*regexp.Regexp{regexp.MustCompile("'ALTER' 'INDEX'")}},
				{name: "rename_table", stmt: "rename_stmt", match: []*regexp.Regexp{regexp.MustCompile("'ALTER' 'TABLE' .* 'RENAME' 'TO'")}},
				{name: "revoke_stmt", inline: []string{"privileges", "privilege_list", "privilege", "privilege_target", "grantee_list"}},
				{name: "rollback_transaction", stmt: "transaction_stmt", inline: []string{"opt_transaction"}, match: []*regexp.Regexp{regexp.MustCompile("'ROLLBACK'")}},
				{name: "savepoint_stmt", inline: []string{"savepoint_name"}},
				{
					name:    "select_stmt",
					inline:  []string{"select_no_parens", "simple_select", "opt_sort_clause", "select_limit"},
					nosplit: true,
				},
				{
					name:   "set_time_zone",
					stmt:   "set_stmt",
					inline: []string{"set_rest", "set_rest_more", "generic_set"},
					match:  []*regexp.Regexp{regexp.MustCompile("'SET' 'TIME'")},
				},
				{
					name:    "set_database",
					stmt:    "set_stmt",
					inline:  []string{"set_rest", "set_rest_more", "generic_set"},
					match:   []*regexp.Regexp{regexp.MustCompile("'SET' var_name .* var_list")},
					replace: map[string]string{"var_name": "'DATABASE'", "var_list": "database_name"},
					unlink:  []string{"database_name"},
				},
				{name: "set_transaction", stmt: "set_stmt", inline: []string{"set_rest", "transaction_mode_list", "transaction_iso_level", "transaction_user_priority"}, replace: map[string]string{" | set_rest_more": ""}, match: []*regexp.Regexp{regexp.MustCompile("'TRANSACTION'")}},
				{name: "show_columns", stmt: "show_stmt", match: []*regexp.Regexp{regexp.MustCompile("'SHOW' 'COLUMNS'")}, replace: map[string]string{"var_name": "table_name"}, unlink: []string{"table_name"}},
				{name: "show_constraints", stmt: "show_stmt", match: []*regexp.Regexp{regexp.MustCompile("'SHOW' 'CONSTRAINTS'")}, replace: map[string]string{"var_name": "table_name"}, unlink: []string{"table_name"}},
				{name: "show_create_table", stmt: "show_stmt", match: []*regexp.Regexp{regexp.MustCompile("'SHOW' 'CREATE' 'TABLE'")}, replace: map[string]string{"var_name": "table_name"}, unlink: []string{"table_name"}},
				{name: "show_databases", stmt: "show_stmt", match: []*regexp.Regexp{regexp.MustCompile("'SHOW' 'DATABASES'")}},
				{
					name:   "show_grants",
					stmt:   "show_stmt",
					inline: []string{"on_privilege_target_clause", "privilege_target", "for_grantee_clause", "grantee_list", "table_pattern_list", "name_list"},
					match:  []*regexp.Regexp{regexp.MustCompile("'SHOW' 'GRANTS'")},
					replace: map[string]string{
						"table_pattern":                 "table_name",
						"'DATABASE' name ( ',' name )*": "'DATABASE' database_name ( ',' database_name )*",
						"'FOR' name ( ',' name )*":      "'FOR' user_name ( ',' user_name )*",
					},
					unlink: []string{"table_name", "database_name", "user_name"},
				},
				{name: "show_index", stmt: "show_stmt", match: []*regexp.Regexp{regexp.MustCompile("'SHOW' 'INDEX'")}, replace: map[string]string{"var_name": "table_name"}, unlink: []string{"table_name"}},
				{name: "show_keys", stmt: "show_stmt", match: []*regexp.Regexp{regexp.MustCompile("'SHOW' 'KEYS'")}},
				{name: "show_tables", stmt: "show_stmt", match: []*regexp.Regexp{regexp.MustCompile("'SHOW' 'TABLES'")}},
				{name: "show_timezone", stmt: "show_stmt", match: []*regexp.Regexp{regexp.MustCompile("'SHOW' 'TIME' 'ZONE'")}},
				{name: "show_transaction", stmt: "show_stmt", match: []*regexp.Regexp{regexp.MustCompile("'SHOW' 'TRANSACTION'")}},
				{name: "table_constraint", inline: []string{"constraint_elem", "opt_storing", "storing"}},
				{
					name:    "truncate_stmt",
					inline:  []string{"opt_table", "relation_expr_list", "relation_expr", "opt_drop_behavior"},
					replace: map[string]string{"'ONLY' '(' qualified_name ')'": "", "'ONLY' qualified_name": "", "qualified_name": "table_name", "'*'": "", "'CASCADE'": "", "'RESTRICT'": ""},
					unlink:  []string{"table_name"},
				},
				{name: "update_stmt", inline: []string{"relation_expr_opt_alias", "set_clause_list", "set_clause", "single_set_clause", "multiple_set_clause", "ctext_row", "ctext_expr_list", "ctext_expr", "from_clause", "from_list", "where_clause", "returning_clause"}},
				{name: "upsert_stmt", stmt: "insert_stmt", inline: []string{"insert_target", "insert_rest", "returning_clause"}, match: []*regexp.Regexp{regexp.MustCompile("'UPSERT'")}},
			}

			for _, s := range specs {
				if s.name != "truncate_stmt" {
					//continue
				}
				wg.Add(1)
				go func(s stmtSpec) {
					defer wg.Done()
					if filter != "" && filter != s.name {
						return
					}
					if s.stmt == "" {
						s.stmt = s.name
					}
					g, err := runParse(br(), s.inline, s.stmt, false, s.nosplit, s.match, s.exclude)
					if err != nil {
						log.Fatalf("%s: %s\n%s", s.name, err, g)
					}
					if printBNF {
						fmt.Printf("%s: (PRE REPLACE)\n\n%s\n", s.name, g)
					}
					for from, to := range s.replace {
						g = bytes.Replace(g, []byte(from), []byte(to), -1)
					}
					for from, to := range s.regreplace {
						re := regexp.MustCompile(from)
						g = re.ReplaceAll(g, []byte(to))
					}
					if printBNF {
						fmt.Printf("%s: (POST REPLACE)\n\n%s\n", s.name, g)
						return
					}
					rr, err := runRR(s.name, bytes.NewReader(g))
					if err != nil {
						log.Fatalf("%s: %s\n%s", s.name, err, g)
					}
					body, err := extract.ExtractTag(bytes.NewReader(rr), "svg")
					if err != nil {
						log.Fatal(s.name, err)
					}
					body = strings.Replace(body, `<a xlink:href="#`, `<a xlink:href="sql-grammar.html#`, -1)
					name := strings.Replace(s.name, "_stmt", "", 1)
					for _, u := range s.unlink {
						s := fmt.Sprintf(`<a xlink:href="sql-grammar.html#%s" xlink:title="%s">((?s).*?)</a>`, u, u)
						link := regexp.MustCompile(s)
						body = link.ReplaceAllString(body, "$1")
					}
					if err := ioutil.WriteFile(filepath.Join(baseDir, fmt.Sprintf("%s.html", name)), []byte(body), 0644); err != nil {
						log.Fatal(s.name, err)
					}
				}(s)
			}
			wg.Wait()
		},
	}

	rootCmd.Flags().StringVar(&addr, "addr", "https://raw.githubusercontent.com/cockroachdb/cockroach/master/sql/parser/sql.y", "Location of sql.y file. Can also specify a local file.")
	rootCmd.Flags().StringVar(&baseDir, "base", filepath.Join("..", "_includes", "sql", "diagrams"), "Base directory for html output.")
	rootCmd.Flags().StringVar(&filter, "filter", "", "Filter statement names")
	rootCmd.Flags().BoolVar(&printBNF, "bnf", false, "Print BNF only; don't generate railroad diagrams")

	rootCmd.AddCommand(cmdBNF, cmdParse, cmdRR, cmdBody, cmdFuncs)
	rootCmd.PersistentFlags().StringVar(&outputPath, "out", "", "Output path; stdout if empty.")
	rootCmd.PersistentFlags().StringVar(&inputPath, "in", "", "Input path; stdin if empty.")
	rootCmd.Execute()
}

type stmtSpec struct {
	name           string
	stmt           string // if unspecified, uses name
	inline         []string
	replace        map[string]string
	regreplace     map[string]string
	match, exclude []*regexp.Regexp
	unlink         []string
	nosplit        bool
}

func runBNF(addr string) ([]byte, error) {
	log.Printf("generate BNF: %s", addr)
	return extract.GenerateBNF(addr)
}

func runParse(r io.Reader, inline []string, topStmt string, descend, nosplit bool, match, exclude []*regexp.Regexp) ([]byte, error) {
	log.Printf("parse: %s, inline: %s, descend: %v", topStmt, inline, descend)
	g, err := extract.ParseGrammar(r)
	if err != nil {
		log.Fatal(err)
	}
	if err := g.Inline(inline...); err != nil {
		log.Fatal(err)
	}
	b, err := g.ExtractProduction(topStmt, descend, nosplit, match, exclude)
	b = bytes.Replace(b, []byte("IDENT"), []byte("identifier"), -1)
	b = bytes.Replace(b, []byte("_LA"), []byte(""), -1)
	return b, err
}

func runRR(name string, r io.Reader) ([]byte, error) {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	html, err := extract.GenerateRR(b)
	if err != nil {
		return nil, err
	}
	log.Printf("%s: generated railroad diagram", name)
	s, err := extract.XHTMLtoHTML(bytes.NewReader(html))
	return []byte(s), err
}
