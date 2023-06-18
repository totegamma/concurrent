import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { Register } from './pages/Register'
import { Box, Paper } from '@mui/material'

function App(): JSX.Element {
    return (
        <Box sx={{width: '100vw', height: '100dvh', display: 'flex', justifyContent: 'center', padding: '30px', backgroundColor: '#333'}}>
            <Paper sx={{width: '900px', padding: '20px'}}>
                <BrowserRouter>
                    <Routes>
                        <Route path="/register" element={<Register />} />
                    </Routes>
                </BrowserRouter>
            </Paper>
        </Box>
    )
}

export default App
