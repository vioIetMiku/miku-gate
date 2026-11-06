"""Fixed terminal quick setup, idempotent exports and no subscription noise."""
import contextlib
import io
import unittest
from unittest.mock import patch
import quick
import inbound_wizard


def result(count=11,ready=None,status='done'):
    return {'job':'same-job','complete':count==11,'assigned':count,'ready':count if ready is None else ready,
            'status':status,'links':[f'vless://same-client@192.0.2.1:{30000+i}?security=reality&flow=xtls-rprx-vision#node-{i}' for i in range(count)]}


class Panel:
    def __init__(self,states):self.states=iter(states);self.calls=[]
    def request(self,method,path,body=None):
        self.calls.append((method,path,body));value=next(self.states)
        if isinstance(value,BaseException):raise value
        return value


class TerminalQuickTests(unittest.TestCase):
    def test_repeated_complete_batch_exports_original_eleven_without_prompts(self):
        p=Panel([result(11,ready=4)])
        with patch('builtins.input',side_effect=AssertionError('no prompts')),contextlib.redirect_stdout(io.StringIO()) as out:
            inbound_wizard.quick_menu(p)
        self.assertEqual(p.calls,[('POST','api/quick-setup/terminal',None)])
        self.assertEqual(out.getvalue().count('vless://'),11)
        self.assertIn('需要更多节点',out.getvalue());self.assertNotIn('订阅',out.getvalue())

    def test_fresh_batch_waits_for_links_and_outputs_no_global_subscription(self):
        p=Panel([{'job':'same-job'},result(2,status='running'),result()])
        with patch.object(quick.time,'sleep'),patch('builtins.input',side_effect=AssertionError('no prompts')),contextlib.redirect_stdout(io.StringIO()) as out:
            quick.terminal_quick(p)
        self.assertEqual(out.getvalue().count('vless://'),11)
        self.assertIn('VLESS + TCP + REALITY + Vision',out.getvalue())
        self.assertTrue(all(path=='api/quick-setup/terminal' for _,path,_ in p.calls))

    def test_ctrl_c_keeps_background_job_and_exports_partial_results(self):
        p=Panel([{'job':'same-job'},KeyboardInterrupt(),result(3,status='running')])
        with contextlib.redirect_stdout(io.StringIO()) as out:quick.terminal_quick(p)
        self.assertEqual(out.getvalue().count('vless://'),3)
        self.assertEqual(sum(method=='POST' for method,_,_ in p.calls),1)

    def test_plain_quick_command_uses_fixed_flow_without_domain_picker(self):
        with patch.object(quick,'terminal_quick') as fixed,patch.object(quick,'choose_domain',side_effect=AssertionError('no domain picker')):
            quick.command([])
        fixed.assert_called_once_with(background=False)


if __name__=='__main__':unittest.main()
